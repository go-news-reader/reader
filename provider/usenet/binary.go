package usenet

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"

	// Register the common decoders so Thumbnail can decode downloaded images.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	goimages "github.com/go-images/images"
	gonzb "github.com/go-newsgroups/nzb"
	"github.com/go-newsgroups/par2"
)

// FetchNZB parses an .nzb document, downloads every file it describes over a
// single NNTP connection (yEnc-decoding and reassembling each), and returns a
// map of file name to bytes. This is the "binary download" behind a search
// result's Link.
func (p *Provider) FetchNZB(ctx context.Context, nzbData []byte) (map[string][]byte, error) {
	doc, err := gonzb.Parse(nzbData)
	if err != nil {
		return nil, err
	}
	c, err := p.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	files := make(map[string][]byte, len(doc.Files))
	for _, f := range doc.Files {
		data, name, err := gonzb.DownloadFile(ctx, c, f)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}

// recoverySet is the slice of *par2.RecoverySet AutoPAR needs; an interface so
// tests can drive the verify/repair error branches.
type recoverySet interface {
	Verify(files map[string][]byte) (*par2.VerifyResult, error)
	Repair(files map[string][]byte) (map[string][]byte, error)
}

// AutoPAR verifies the downloaded data files against a parsed PAR2 recovery set
// and, when the set has enough recovery slices, repairs missing/damaged files.
// It returns the (possibly repaired) data files and the final verification
// result. Files are unchanged when already complete.
//
// The caller obtains rs by splitting the ".par2" blobs out of a downloaded set
// (see SplitPAR2) and calling par2.Parse on them.
func AutoPAR(rs *par2.RecoverySet, data map[string][]byte) (map[string][]byte, *par2.VerifyResult, error) {
	return autoPAR(rs, data)
}

func autoPAR(rs recoverySet, data map[string][]byte) (map[string][]byte, *par2.VerifyResult, error) {
	vr, err := rs.Verify(data)
	if err != nil {
		return nil, nil, err
	}
	if vr.Complete || !vr.Repairable {
		return data, vr, nil
	}
	repaired, err := rs.Repair(data)
	if err != nil {
		return nil, nil, err
	}
	final, err := rs.Verify(repaired)
	if err != nil {
		return nil, nil, err
	}
	return repaired, final, nil
}

// SplitPAR2 separates a downloaded file set into the raw ".par2" recovery blobs
// and the remaining data files, so the caller can par2.Parse the blobs and feed
// the data map to AutoPAR.
func SplitPAR2(files map[string][]byte) (par2Blobs [][]byte, data map[string][]byte) {
	data = make(map[string][]byte)
	for name, b := range files {
		if strings.HasSuffix(strings.ToLower(name), ".par2") {
			par2Blobs = append(par2Blobs, b)
			continue
		}
		data[name] = b
	}
	return par2Blobs, data
}

// resizeImage and encodeJPEG are package vars so tests can drive Thumbnail's
// otherwise-untriggerable error branches.
var (
	resizeImage = goimages.Resize
	encodeJPEG  = func(buf *bytes.Buffer, img image.Image) error {
		return jpeg.Encode(buf, img, &jpeg.Options{Quality: 85})
	}
)

// Thumbnail decodes an image file and returns a JPEG thumbnail scaled so its
// longest side is at most maxDim pixels (aspect preserved), using go-images for
// the resize. Errors if the bytes are not a decodable image.
func Thumbnail(data []byte, maxDim int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := thumbDims(b.Dx(), b.Dy(), maxDim)
	resized, err := resizeImage(img, w, h, goimages.Bilinear)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := encodeJPEG(&buf, resized); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// thumbDims scales (srcW, srcH) so the longest side is at most maxDim, never
// upscaling and never returning a zero dimension.
func thumbDims(srcW, srcH, maxDim int) (int, int) {
	if srcW <= maxDim && srcH <= maxDim {
		return srcW, srcH
	}
	if srcW >= srcH {
		h := srcH * maxDim / srcW
		if h < 1 {
			h = 1
		}
		return maxDim, h
	}
	w := srcW * maxDim / srcH
	if w < 1 {
		w = 1
	}
	return w, maxDim
}
