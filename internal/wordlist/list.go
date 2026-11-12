package wordlist

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

const (
	// RemoteWordlistBase is where the wordlist manifests are published. It can be overridden with the
	// KITERUNNER_WORDLIST_URL environment variable, e.g. to use a mirror
	RemoteWordlistBase = "https://wordlists.assetnote.io/data/"
)

var (
	// the manifests published at RemoteWordlistBase. technologies.json is not included: it lists hosts per
	// technology, which are scan targets rather than wordlists
	wordlists = []string{
		"automated.json",
		"manual.json",
		"kiterunner.json",
	}

	ErrNotFound = fmt.Errorf("wordlist not found")
)

type Format int

const (
	Unknown Format = iota
	Pretty
	Plain
	JSON
)

var (
	ErrInvalidFormat = fmt.Errorf("unknown format")
)

func FormatFromString(in string) (Format, error) {
	switch strings.ToLower(in) {
	case "pretty":
		return Pretty, nil
	case "plain", "text":
		return Plain, nil
	case "json":
		return JSON, nil
	}
	return Unknown, ErrInvalidFormat
}

type ListOptions struct {
	Output Format
}

type ListOption func(o *ListOptions)

func OutputFormat(v Format) ListOption {
	return func(o *ListOptions) {
		o.Output = v
	}
}

func NewListOptions(opts ...ListOption) *ListOptions {
	l := &ListOptions{}
	for _, v := range opts {
		v(l)
	}
	return l
}

func List(ctx context.Context, opts ...ListOption) error {
	o := NewListOptions(opts...)

	if err := CreateLocalCache(); err != nil {
		return fmt.Errorf("failed to create local cache dir: %w", err)
	}

	wls, err := GetRemoteWordlists()
	if err != nil {
		return fmt.Errorf("failed to get remote wordlists: %w", err)
	}

	sort.Slice(wls, func(i, j int) bool {
		return wls[i].Filename < wls[j].Filename
	})

	wls, err = CheckAllCached(wls)
	if err != nil {
		return fmt.Errorf("failed to check local cache: %w", err)
	}

	switch o.Output {
	case Plain:
		for _, v := range wls {
			fmt.Println(TabString(v.Shortname, v.Filename, v.Source, strconv.Itoa(v.LineCount), v.FileSize, strconv.FormatBool(v.Cached)))
		}
	case JSON:
		if err := json.NewEncoder(os.Stdout).Encode(wls); err != nil {
			return fmt.Errorf("failed to encode wordlist: %w", err)
		}
	case Pretty:
		fallthrough
	default:
		table := tablewriter.NewTable(os.Stdout,
			tablewriter.WithRendition(tw.Rendition{Symbols: tw.NewSymbols(tw.StyleASCII)}),
		)
		table.Header("alias", "filename", "source", "count", "filesize", "cached")
		for _, v := range wls {
			if err := table.Append([]string{v.Shortname, v.Filename, v.Source, strconv.Itoa(v.LineCount), v.FileSize, strconv.FormatBool(v.Cached)}); err != nil {
				return fmt.Errorf("failed to build wordlist table: %w", err)
			}
		}
		if err := table.Render(); err != nil {
			return fmt.Errorf("failed to render wordlist table: %w", err)
		}
	}

	return nil
}

// getDownloadLink will extract the url from the html embedded thing that shubs did
// "Download": "<a href='https://s3.amazonaws.com/assetnote-wordlists/./data/automated/httparchive_aspx_asp_cfm_svc_ashx_asmx_2021_02_28.txt'>Download</a>"
func getDownloadLink(html string) string {
	html = strings.Replace(html, `<a href='`, "", -1)
	html = strings.Replace(html, `'>Download</a>`, "", -1)
	// handle the ./ in the URL because golang doesn't automatically collapse it
	html = strings.Replace(html, "./", "", -1)
	return html
}

// nameConvert will convert the filename like httparchive_apiroutes_2021_01_28.txt into a shortname like
// apiroutes-210128. If you supply it weird input, it'll do weird things.
func nameConvert(in string) string {
	// compiled kiterunner wordlists are distributed as routes-small.kite.tar.gz
	if strings.HasSuffix(in, kiteArchiveSuffix) {
		return strings.TrimSuffix(in, kiteArchiveSuffix)
	}
	if strings.HasSuffix(in, schemaArchiveSuffix) {
		return strings.TrimSuffix(in, schemaArchiveSuffix)
	}
	// remove extension
	in = strings.TrimSuffix(in, filepath.Ext(in))

	split := strings.Split(in, "_")
	if len(split) < 4 {
		return in
	}

	y, m, d := split[len(split)-3], split[len(split)-2], split[len(split)-1]
	if len(y) == 4 {
		y = y[2:]
	}

	// y,m,d so its sortable
	return fmt.Sprintf("%s-%s%s%s", split[1], y, m, d)
}

func TabString(fields ...string) string {
	return strings.Join(fields, "\t")
}
