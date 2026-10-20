package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const (
	abortTimeout = 30 * time.Second
	maxParts     = 10000 // S3's limit for one multipart upload
)

// Upload stores size bytes of f under key: one PUT below PartSize, otherwise a
// multipart upload with PartConcurrency parts in flight. A failed multipart
// upload is aborted so it leaves no parts behind.
func (c *Client) Upload(ctx context.Context, key string, f io.ReaderAt, size int64, opt PutOptions) error {
	if size < PartSize {
		return c.Put(ctx, key, io.NewSectionReader(f, 0, size), size, opt)
	}
	if size > maxParts*PartSize {
		return fmt.Errorf("s3: %s is %d bytes, larger than the %d parts of %d MiB an upload can hold", key, size, maxParts, PartSize>>20)
	}
	id, err := c.createMultipart(ctx, key, opt)
	if err != nil {
		return err
	}
	etags, err := c.uploadParts(ctx, key, id, f, size)
	if err == nil {
		err = c.complete(ctx, key, id, etags)
	}
	if err == nil {
		return nil
	}
	// The caller's context may be what failed, so the abort gets its own.
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	if aerr := c.Abort(actx, key, id); aerr != nil {
		return errors.Join(err, fmt.Errorf("s3: abort the unfinished upload of %s: %w", key, aerr))
	}
	return err
}

type initiateResult struct {
	UploadId string
}

func (c *Client) createMultipart(ctx context.Context, key string, opt PutOptions) (string, error) {
	var res initiateResult
	k := call{method: http.MethodPost, key: key, query: url.Values{"uploads": {""}}, header: putHeader(opt)}
	if err := c.doXML(ctx, k, &res); err != nil {
		return "", err
	}
	if res.UploadId == "" {
		return "", fmt.Errorf("s3: starting the upload of %s returned no upload ID", key)
	}
	return res.UploadId, nil
}

func (c *Client) uploadParts(ctx context.Context, key, id string, f io.ReaderAt, size int64) ([]string, error) {
	n := int((size + PartSize - 1) / PartSize)
	etags := make([]string, n)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	parts := make(chan int)
	for range min(PartConcurrency, n) {
		wg.Go(func() {
			for i := range parts {
				off := int64(i) * PartSize
				etag, err := c.uploadPart(ctx, key, id, i+1, io.NewSectionReader(f, off, min(PartSize, size-off)))
				if err != nil {
					once.Do(func() {
						first = err
						cancel()
					})
					continue
				}
				etags[i] = etag
			}
		})
	}
feed:
	for i := range n {
		select {
		case parts <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(parts)
	wg.Wait()
	if first != nil {
		return nil, first
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return etags, nil
}

func (c *Client) uploadPart(ctx context.Context, key, id string, number int, part *io.SectionReader) (string, error) {
	q := url.Values{"partNumber": {strconv.Itoa(number)}, "uploadId": {id}}
	resp, err := c.do(ctx, call{method: http.MethodPut, key: key, query: q, body: part, size: part.Size(), payload: UnsignedPayload})
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", fmt.Errorf("s3: part %d of %s came back without an ETag", number, key)
	}
	return etag, nil
}

type completeRequest struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []completePart `xml:"Part"`
}

type completePart struct {
	PartNumber int
	ETag       string
}

func (c *Client) complete(ctx context.Context, key, id string, etags []string) error {
	req := completeRequest{Parts: make([]completePart, len(etags))}
	for i, e := range etags {
		req.Parts[i] = completePart{PartNumber: i + 1, ETag: e}
	}
	body, err := xmlBody(req)
	if err != nil {
		return err
	}
	k := call{method: http.MethodPost, key: key, query: url.Values{"uploadId": {id}}, body: bytes.NewReader(body), size: int64(len(body)), payload: HashHex(body)}
	return c.doXML(ctx, k, nil)
}
