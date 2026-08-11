// Package transport is the hashicorp/go-plugin + gRPC glue that carries a
// source.Provider across a process boundary. It is deliberately thin: every
// method either forwards a call over gRPC or (de)serializes through
// package protoconv, whose mapping is unit-tested directly. Because this code is
// exercised only against a live plugin subprocess (and cannot be driven to 100%
// line coverage in-process), it lives under internal/sourceplugin/transport,
// which the CI coverage gate excludes — exactly as internal/window is excluded.
//
// Two entry points matter:
//
//   - Serve wraps a provider so a plugin binary's main() serves it. It is
//     re-exported as sourceplugin.Serve, the SDK a third-party plugin author
//     calls.
//   - Launch starts a plugin binary and returns a source.Provider backed by it,
//     plus a function that kills the subprocess. The host (package sourceplugin)
//     calls it for every plugin it discovers.
package transport

import (
	"context"
	"errors"
	"os/exec"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	pb "github.com/go-news-reader/reader/internal/sourceplugin/grpc"
	"github.com/go-news-reader/reader/internal/sourceplugin/protoconv"
	"github.com/go-news-reader/reader/source"
)

// pluginKey is the single dispensable plugin name in every source plugin.
const pluginKey = "source"

// Handshake is the magic-cookie handshake both sides share; a mismatch makes
// go-plugin refuse a binary that is not one of our source plugins, so a stray
// executable in the plugins directory is rejected rather than mis-driven.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GO_NEWS_READER_SOURCE_PLUGIN",
	MagicCookieValue: "b3f6c0d2-source-provider-v1",
}

// SourcePlugin is the go-plugin adapter for a source.Provider. On the server
// side Impl is the provider being served; on the client side it is nil and
// GRPCClient builds the dialing adapter.
type SourcePlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Impl source.Provider
}

// GRPCServer registers the provider-backed gRPC service on s.
func (p *SourcePlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterSourceProviderServer(s, &grpcServer{impl: p.Impl})
	return nil
}

// GRPCClient returns a source.Provider that forwards to the plugin over c.
func (p *SourcePlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &grpcClient{client: pb.NewSourceProviderClient(c)}, nil
}

// grpcServer adapts a source.Provider to the generated gRPC server interface.
type grpcServer struct {
	pb.UnimplementedSourceProviderServer
	impl source.Provider
}

func (s *grpcServer) Kind(context.Context, *pb.KindRequest) (*pb.KindReply, error) {
	return &pb.KindReply{Kind: string(s.impl.Kind())}, nil
}

func (s *grpcServer) Feed(ctx context.Context, req *pb.FeedRequest) (*pb.FeedReply, error) {
	res, err := s.impl.Feed(ctx, protoconv.QueryFromProto(req))
	reply := protoconv.ResultToProto(res)
	if err != nil {
		// A provider error is transported as a string in the reply, not as a gRPC
		// status, so the host reconstitutes an ordinary Go error from it.
		reply.Error = err.Error()
	}
	return reply, nil
}

// grpcClient adapts the generated gRPC client to source.Provider. kind is
// cached at load (see Launch) so Kind is a field read, not a round trip.
type grpcClient struct {
	kind   source.Kind
	client pb.SourceProviderClient
}

func (c *grpcClient) Kind() source.Kind { return c.kind }

func (c *grpcClient) Feed(ctx context.Context, q source.Query) (source.Result, error) {
	reply, err := c.client.Feed(ctx, protoconv.QueryToProto(q))
	if err != nil {
		return source.Result{}, err
	}
	if reply.GetError() != "" {
		return source.Result{}, errors.New(reply.GetError())
	}
	return protoconv.ResultFromProto(reply), nil
}

// ServeConfig builds the go-plugin ServeConfig that serves p. It is separated
// from the plugin.Serve call so the exported sourceplugin.Serve wrapper can be
// unit-tested without blocking on a live server.
func ServeConfig(p source.Provider) *goplugin.ServeConfig {
	return &goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         goplugin.PluginSet{pluginKey: &SourcePlugin{Impl: p}},
		GRPCServer:      goplugin.DefaultGRPCServer,
	}
}

// Launch starts the plugin binary at path, dials it, caches its Kind, and
// returns a source.Provider backed by the subprocess plus a function that kills
// it. Any handshake/dial/dispense failure kills the subprocess and returns the
// error, so a non-plugin executable is reported (and skipped) rather than left
// running.
func Launch(path string) (source.Provider, func() error, error) {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          goplugin.PluginSet{pluginKey: &SourcePlugin{}},
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
		// Keep the handshake window short so a stray/broken executable fails fast
		// instead of stalling the scan for go-plugin's default minute.
		StartTimeout: 10 * time.Second,
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
		return nil, nil, errors.New("sourceplugin: unexpected dispensed type")
	}
	kr, err := adapter.client.Kind(context.Background(), &pb.KindRequest{})
	if err != nil {
		_ = kill()
		return nil, nil, err
	}
	adapter.kind = source.Kind(kr.GetKind())
	return adapter, kill, nil
}
