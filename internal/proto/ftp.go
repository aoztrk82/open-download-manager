package proto

import (
	"context"
	"crypto/tls"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// FTP implements Source for ftp and ftps (explicit TLS). Every Open dials its
// own control connection so segments can download in parallel.
type FTP struct{}

func NewFTP() *FTP { return &FTP{} }

func (f *FTP) dial(ctx context.Context, rawURL string) (*ftp.ServerConn, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", err
	}
	host := u.Host
	if u.Port() == "" {
		host += ":21"
	}
	opts := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(30 * time.Second)}
	if strings.EqualFold(u.Scheme, "ftps") {
		opts = append(opts, ftp.DialWithExplicitTLS(&tls.Config{ServerName: u.Hostname()}))
	}
	c, err := ftp.Dial(host, opts...)
	if err != nil {
		return nil, "", err
	}
	user, pass := "anonymous", "anonymous@"
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	if err := c.Login(user, pass); err != nil {
		c.Quit()
		return nil, "", err
	}
	p, _ := url.PathUnescape(u.Path)
	return c, p, nil
}

func (f *FTP) Probe(ctx context.Context, r Request) (Info, error) {
	c, p, err := f.dial(ctx, r.URL)
	if err != nil {
		return Info{}, err
	}
	defer c.Quit()
	size, err := c.FileSize(p)
	if err != nil {
		return Info{}, err
	}
	// Resume support is signalled by REST STREAM; assume yes when MLST/SIZE work.
	return Info{Size: size, AcceptRanges: true, Filename: NameFromURL(r.URL), FinalURL: r.URL}, nil
}

type ftpReader struct {
	io.Reader
	resp *ftp.Response
	conn *ftp.ServerConn
}

func (r *ftpReader) Close() error {
	r.resp.Close()
	r.conn.Quit()
	return nil
}

func (f *FTP) Open(ctx context.Context, r Request, offset, length int64) (io.ReadCloser, error) {
	c, p, err := f.dial(ctx, r.URL)
	if err != nil {
		return nil, err
	}
	resp, err := c.RetrFrom(p, uint64(offset))
	if err != nil {
		c.Quit()
		return nil, err
	}
	var rd io.Reader = resp
	if length >= 0 {
		rd = io.LimitReader(resp, length)
	}
	return &ftpReader{Reader: rd, resp: resp, conn: c}, nil
}
