//go:build live

package outline

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"golang.getoutline.org/sdk/x/configurl"
)

func TestLiveSSConf(t *testing.T) {
	key := os.Getenv("OUTLINE_SSCONF")
	if key == "" {
		t.Skip("OUTLINE_SSCONF not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exp, err := ExpandAccessKey(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("expanded host part after @: %s", exp[len(exp)-40:])
	p := configurl.NewDefaultProviders()
	d, err := p.NewStreamDialer(ctx, exp)
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.DialStream(ctx, "ifconfig.me:80")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte("GET / HTTP/1.1\r\nHost: ifconfig.me\r\nConnection: close\r\n\r\n"))
	buf := make([]byte, 100)
	n, err := io.ReadAtLeast(c, buf, 1)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	t.Logf("OK n=%d", n)
}
