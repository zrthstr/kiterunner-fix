package wordlist

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/zrthstr/kiterunner-fix/pkg/log"
)

func GetLocalDirPanic() string {
	ret, err := GetLocalDir()
	if err != nil {
		panic(err)
	}
	return ret
}

// GetLocalDir returns the wordlist cache directory: $XDG_CACHE_HOME/kiterunner/wordlists if set, otherwise
// ~/.cache/kiterunner/wordlists. $HOME is respected (os/user ignored it, which broke containers and CI)
func GetLocalDir() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "kiterunner", "wordlists"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to find home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "kiterunner", "wordlists"), nil
}

func CreateLocalCache() error {
	newpath, err := GetLocalDir()
	if err != nil {
		return fmt.Errorf("failed to get local dir: %w", err)
	}
	log.Debug().Str("path", newpath).Msg("creating directory")
	if err := os.MkdirAll(newpath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create dir: %w", err)
	}

	return nil
}

func GetLocalDirListing() ([]WordlistMetadata, error) {
	dir, err := GetLocalDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get local dir: %w", err)
	}

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read dir: %w", err)
	}

	ret := make([]WordlistMetadata, 0)
	for _, v := range files {
		name := v.Name()
		// only wordlists: .kite files in the cache are compiled or unpacked from them, and partial downloads
		// are temporary. Archives that are not the preferred source for their alias are left over from older
		// versions (the official routes-small.kite has no parameters)
		if strings.HasSuffix(name, ".kite") || strings.Contains(name, ".download-") || strings.Contains(name, ".compiling") {
			continue
		}
		if want, ok := preferredSource[nameConvert(name)]; ok && name != want {
			continue
		}
		r := WordlistMetadata{
			Shortname: nameConvert(v.Name()),
			Filename:  v.Name(),
			Cached:    true,
			FileSize:  humanize.Bytes(uint64(v.Size())),
			Source:    "local",
		}
		ret = append(ret, r)
	}

	return ret, nil
}
