package wordlist

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zrthstr/kiterunner-fix/pkg/log"
	"github.com/zrthstr/kiterunner-fix/pkg/proute"
)

type WordlistMetadata struct {
	Shortname string  `json:"Shortname,omitempty"`
	Filename  string  `json:"Filename,omitempty"`
	LineCount int     `json:"Line Count,omitempty"`
	FileSize  string  `json:"File Size,omitempty"`
	Date      float64 `json:"Date,omitempty"`
	Download  string  `json:"Download,omitempty"`
	Source    string  `json:"Source,omitempty"`
	Cached    bool    `json:"Cached"`
}

const kiteArchiveSuffix = ".kite.tar.gz"

// httpClient is used for manifests and downloads. There is no overall timeout because wordlists can be large,
// but connecting and waiting for the response headers must not hang forever
var httpClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	},
}

func remoteBase() string {
	if v := os.Getenv("KITERUNNER_WORDLIST_URL"); v != "" {
		return strings.TrimSuffix(v, "/") + "/"
	}
	return RemoteWordlistBase
}

// get performs a GET and fails on any non 200 response, so an error page is never mistaken for content
func get(url string) (*http.Response, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	return resp, nil
}

// IsKite reports whether the wordlist is a compiled kiterunner wordlist (a .kite inside a .tar.gz)
func (w WordlistMetadata) IsKite() bool {
	return strings.HasSuffix(w.Filename, kiteArchiveSuffix)
}

// usable reports whether kr can use the wordlist. kiterunner.json also lists the raw swagger dataset and the
// json sources of the kite files, which are inputs for building wordlists rather than wordlists
func (w WordlistMetadata) usable() bool {
	return w.IsKite() || strings.HasSuffix(w.Filename, ".txt")
}

func GetRemoteWordlists() ([]WordlistMetadata, error) {
	ret := make([]WordlistMetadata, 0)

	type data struct {
		Data []WordlistMetadata `json:"data"`
	}

	for _, source := range wordlists {
		resp, err := get(remoteBase() + source)
		if err != nil {
			return nil, fmt.Errorf("failed to get wordlist manifest: %w", err)
		}

		var jsond data
		err = json.NewDecoder(resp.Body).Decode(&jsond)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal wordlist manifest %s: %w", source, err)
		}

		for _, v := range jsond.Data {
			if !v.usable() {
				continue
			}
			v.Download = getDownloadLink(v.Download)
			v.Source = source
			v.Shortname = nameConvert(v.Filename)
			ret = append(ret, v)
		}
	}

	return ret, nil
}

func (w WordlistMetadata) LocalFilenamePanic() string {
	dir, err := GetLocalDir()
	if err != nil {
		panic(fmt.Errorf("failed to get local dir: %w", err))
	}

	outfile := path.Join(dir, w.Filename)
	return outfile
}

func (w WordlistMetadata) LocalKiteFilename() (string, error) {
	dir, err := GetLocalDir()
	if err != nil {
		return "", fmt.Errorf("failed to get local dir: %w", err)
	}

	fn := fmt.Sprintf("%s.kite", strings.Replace(w.Filename, path.Ext(w.Filename), "", -1))
	if w.IsKite() {
		// routes-small.kite.tar.gz is unpacked to routes-small.kite
		fn = strings.TrimSuffix(w.Filename, ".tar.gz")
	}
	outfile := path.Join(dir, fn)
	return outfile, nil
}

func (w WordlistMetadata) LocalFilename() (string, error) {
	dir, err := GetLocalDir()
	if err != nil {
		return "", fmt.Errorf("failed to get local dir: %w", err)
	}

	outfile := path.Join(dir, w.Filename)
	return outfile, nil
}

// Cache will downlad the file and save it to disk
func (w *WordlistMetadata) Cache() error {
	if w.Cached {
		log.Info().Str("wordlist", w.Filename).Str("local", w.LocalFilenamePanic()).Msg("already cached")
		return nil
	}
	if w.Download == "" {
		return fmt.Errorf("invalid wordlist download url")
	}
	outfile, err := w.LocalFilename()
	if err != nil {
		return fmt.Errorf("failed to get local filename: %w", err)
	}
	log.Info().Str("wordlist", w.Filename).Str("Url", w.Download).Str("local", outfile).Msg("caching locally")

	resp, err := get(w.Download)
	if err != nil {
		return fmt.Errorf("failed to get wordlist: %w", err)
	}
	defer resp.Body.Close()

	// write to a temporary file and rename it into place, so an interrupted or failed download is never
	// mistaken for a complete wordlist
	if err := writeAtomic(outfile, resp.Body); err != nil {
		return fmt.Errorf("failed to save wordlist %s: %w", w.Filename, err)
	}

	// anything compiled from a previous copy of this wordlist is stale now
	if kite, err := w.LocalKiteFilename(); err == nil && kite != outfile {
		os.Remove(kite)
	}

	w.Cached = true
	return nil
}

func writeAtomic(dst string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// extractKite unpacks the single .kite file from a kiterunner .kite.tar.gz archive to dst
func extractKite(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("no .kite file found in %s", filepath.Base(archive))
		}
		if err != nil {
			return fmt.Errorf("failed to read archive: %w", err)
		}
		// only a regular .kite file, written to dst: entry names are never used as paths, so a crafted
		// archive can't write outside of the cache directory
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".kite") {
			continue
		}
		return writeAtomic(dst, tr)
	}
}

// API will atttempt to load a compiled kite API from disk.
// If this doesnt exist, then we attempt to compile a local file to disk.
// If this local file doesnt exist, then we download it first and cache it
func (w WordlistMetadata) APIS() (ret proute.APIS, err error) {
	// get the localfile first
	localkite, err := w.LocalKiteFilename()
	if err != nil {
		return ret, fmt.Errorf("failed to get local filename: %w", err)
	}

	ret, err = proute.DecodeAPIProtoFile(localkite)
	if err == nil {
		log.Debug().Str("wordlist", w.Filename).Str("local", localkite).Msg("loaded compiled kite from cache")
		return ret, nil
	}

	if w.IsKite() {
		// a compiled kite wordlist: download the archive and unpack the .kite from it
		if err := w.Cache(); err != nil {
			return ret, fmt.Errorf("failed to cache file: %w", err)
		}
		archive, err := w.LocalFilename()
		if err != nil {
			return ret, fmt.Errorf("failed to get local filename: %w", err)
		}
		if err := extractKite(archive, localkite); err != nil {
			return ret, fmt.Errorf("failed to unpack %s: %w", w.Filename, err)
		}
		ret, err = proute.DecodeAPIProtoFile(localkite)
		if err != nil {
			return ret, fmt.Errorf("failed to decode %s: %w", localkite, err)
		}
		return ret, nil
	}
	// if there's any error, we'll just try and re-compile the wordlist

	// file does not exist so we should try and compile the existing wordlist
	localfile, err := w.LocalFilename()
	if err != nil {
		return ret, fmt.Errorf("failed to get local filename: %w", err)
	}

	// Cache will not perform any operation if its already cached
	if err := w.Cache(); err != nil {
		return ret, fmt.Errorf("failed to cache file: %w", err)
	}

	words, err := w.Words()
	if err != nil {
		return ret, fmt.Errorf("failed to read words from file: %w", err)
	}

	log.Debug().Int("lines", len(words)).Str("name", w.Filename).Msg("loaded file from disk")
	api, err := proute.FromStringSlice(words, localfile)
	if err != nil {
		return ret, fmt.Errorf("failed to convert to kite: %w", err)
	}
	ret = append(ret, api)

	// cache the compiled file for later
	if err := ret.EncodeProtoFile(localkite); err != nil {
		return ret, fmt.Errorf("failed to cache localkite: %w", err)
	}

	return ret, nil
}

// words will attempt to retrieve the words from the cache. If it fails, it will
func (w WordlistMetadata) Words() ([]string, error) {
	if w.IsKite() {
		return nil, fmt.Errorf("%s is a kite wordlist, use it with `kr scan -A %s`", w.Shortname, w.Shortname)
	}
	localfile, err := w.LocalFilename()
	if err != nil {
		return nil, fmt.Errorf("failed to get local filename: %w", err)
	}

	f, err := os.Open(localfile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// cache the file if it doesnt exist yet
			if err := w.Cache(); err != nil {
				return nil, fmt.Errorf("failed to load words when caching file: %w", err)
			}
			f, err = os.Open(localfile)
			if err != nil {
				return nil, fmt.Errorf("failed to load words after caching file: %w", err)
			}
		} else {
			return nil, fmt.Errorf("unexpected err when opening file: %w", err)
		}
	}
	defer f.Close()

	ret := make([]string, 0)
	r := bufio.NewScanner(f)
	for r.Scan() {
		ret = append(ret, strings.TrimSpace(r.Text()))
	}
	return ret, nil
}

// Cached performs a single stat operation against the local directory to see if the file exists
// on disk. This is not efficient, but a few dozen stat calls never hurt anyone.
// If you want to go fast, use CheckAllCached
func (w *WordlistMetadata) IsCached() bool {
	ret, err := GetLocalDir()
	if err != nil {
		return w.Cached
	}

	fullfile := filepath.Join(ret, w.Filename)
	if _, err := os.Stat(fullfile); err == nil {
		w.Cached = true
	} else if os.IsNotExist(err) {
		w.Cached = false
	} else {
		log.Error().Err(err).Msg("failed to check cache for file")
	}
	return w.Cached
}
