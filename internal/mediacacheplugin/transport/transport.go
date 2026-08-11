// Package transport is the hashicorp/go-plugin + gRPC glue that carries a
// mediacache.Cache across a process boundary. It is deliberately thin: every
// method forwards a call over gRPC, translating the two Cache operations (Get,
// Put) to and from their generated protobuf forms. Because this code is
// exercised only against a live plugin subprocess (and cannot be driven to 100%
// line coverage in-process), it lives under internal/mediacacheplugin/transport,
// which the CI coverage gate excludes — exactly as internal/sourceplugin/transport
// and internal/window are excluded.
//
// Two entry points matter:
//
//   - ServeConfig wraps a cache so a plugin binary's main() serves it. It backs
//     the exported mediacacheplugin.Serve, the SDK a plugin author calls.
//   - Launch starts a plugin binary and returns a mediacache.Cache backed by it,
//     plus a function that kills the subprocess. The host (package
//     mediacacheplugin) calls it for the configured cache-plugin binary.
package transport

import (
	"context"
	"errors"
	"os/exec"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pb "github.com/go-news-reader/reader/internal/mediacacheplugin/grpc"
	"github.com/go-news-reader/reader/mediacache"
)

// pluginKey is the single dispensable plugin name in every media-cache plugin.
const pluginKey = "mediacache"

// Handshake is the magic-cookie handshake both sides share; a mismatch makes
// go-plugin refuse a binary that is not one of our media-cache plugins, so a
// stray executable pointed at by the CacheBackend setting is rejected rather than
// mis-driven.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GO_NEWS_READER_MEDIACACHE_PLUGIN",
	MagicCookieValue: "a7e1d4c8-mediacache-v1",
}

// MediaCachePlugin is the go-plugin adapter for a mediacache.Cache. On the
// server side Impl is the cache being served; on the client side it is nil and
// GRPCClient builds the dialing adapter.
type MediaCachePlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl mediacache.Cache
}

// GRPCServer registers the cache-backed gRPC service on s.
func (p *MediaCachePlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterMediaCacheServer(s, &grpcServer{impl: p.Impl})
	return nil
}

// GRPCClient returns a mediacache.Cache that forwards to the plugin over c.
func (p *MediaCachePlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &grpcClient{client: pb.NewMediaCacheClient(c)}, nil
}

// grpcServer adapts a mediacache.Cache to the generated gRPC server interface.
type grpcServer struct {
	pb.UnimplementedMediaCacheServer
	impl mediacache.Cache
}

func (s *grpcServer) Get(_ context.Context, req *pb.GetRequest) (*pb.GetReply, error) {
	data, found := s.impl.Get(req.GetUrl())
	return &pb.GetReply{Data: data, Found: found}, nil
}

func (s *grpcServer) Put(_ context.Context, req *pb.PutRequest) (*pb.PutReply, error) {
	s.impl.Put(req.GetUrl(), req.GetData())
	return &pb.PutReply{}, nil
}

// grpcClient adapts the generated gRPC client to mediacache.Cache. Both methods
// are best-effort (the Cache contract): a transport error on Get is a miss, and a
// transport error on Put is swallowed, since the caller already holds the bytes.
type grpcClient struct {
	client pb.MediaCacheClient
}

func (c *grpcClient) Get(url string) ([]byte, bool) {
	reply, err := c.client.Get(context.Background(), &pb.GetRequest{Url: url})
	if err != nil || !reply.GetFound() {
		return nil, false
	}
	return reply.GetData(), true
}

func (c *grpcClient) Put(url string, data []byte) {
	_, _ = c.client.Put(context.Background(), &pb.PutRequest{Url: url, Data: data})
}

// ServeConfig builds the go-plugin ServeConfig that serves c. It is separated
// from the plugin.Serve call so the exported mediacacheplugin.Serve wrapper can
// be unit-tested without blocking on a live server.
func ServeConfig(c mediacache.Cache) *goplugin.ServeConfig {
	return &goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         goplugin.PluginSet{pluginKey: &MediaCachePlugin{Impl: c}},
		GRPCServer:      goplugin.DefaultGRPCServer,
	}
}

// Launch starts the plugin binary at path, dials it, and returns a
// mediacache.Cache backed by the subprocess plus a function that kills it. Any
// handshake/dial/dispense failure kills the subprocess and returns the error, so
// a non-plugin executable is reported (and the host falls back) rather than left
// running.
func Launch(path string) (mediacache.Cache, func() error, error) {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          goplugin.PluginSet{pluginKey: &MediaCachePlugin{}},
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
		// A stray/broken executable fails fast on its own (it exits or produces no
		// handshake, which the client detects as EOF, not a timeout); this bound only
		// guards a genuinely hung process, so keep enough headroom that a cold plugin
		// start under a heavily loaded CI host is not mistaken for a hang.
		StartTimeout: 30 * time.Second,
	})
	kill := func() error { client.Kill(); return nil }

	rpc, err := client.Client()
	if err != nil {
		_ = kill()
		return nil, nil, err
	}
	raw, err := rpc.Dispense(pluginKey)
	if err != nil {
		_ = kill()
		return nil, nil, err
	}
	adapter, ok := raw.(*grpcClient)
	if !ok {
		_ = kill()
		return nil, nil, errors.New("mediacacheplugin: unexpected dispensed type")
	}
	return adapter, kill, nil
}
