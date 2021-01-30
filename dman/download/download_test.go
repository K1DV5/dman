// -{go test}

package download

import (
	"testing"
	"net/http"
	"net/url"
	// "fmt"
)


func TestFilename(t *testing.T) {
	resp := http.Response{
		Request: &http.Request{},
		Header: http.Header{},
	}
	for rawurl, fromUrl := range map[string]string{
			"http://foo.com/bar.zip": "bar.zip",
			"http://foo.com/bar.zip?foo=bar&baz=bax": "bar.zip",
			"http://foo.com/foo.boo/bar": "bar",
		} {
		URL, _ := url.Parse(rawurl)
		resp.Request.URL = URL
		if fname := getFilename(&resp); fname != fromUrl {
			t.Errorf("Wrong filename from URL: %s != %s", fname, fromUrl)
		}
	}
	resp.Header.Add("Content-Disposition", "attachment; filename=foo.zip")
	fromHeader := "foo.zip"
	if fname := getFilename(&resp); fname != fromHeader {
		t.Errorf("Wrong filename from Header: %s != %s", fname, fromHeader)
	}
}

func TestReadableSize(t *testing.T) {
	for raw, readable := range map[int64]string{
			1024: "1.00KB",
			1024 * 1024: "1.00MB",
			1024 * 1024 * (1024 + 512): "1.50GB",
		} {
		if size := readableSize(raw); size != readable {
			t.Errorf("Wrong readable size: %s != %s", size, readable)
		}
	}
}
