package kitebuilder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	errors2 "github.com/zrthstr/kiterunner-fix/pkg/errors"
	"github.com/zrthstr/kiterunner-fix/pkg/kitebuilder"
	"github.com/zrthstr/kiterunner-fix/pkg/log"
	"github.com/zrthstr/kiterunner-fix/pkg/proute"
)

type ScanOptions struct {
	Debug bool
}

func NewDefaultScanOptions() *ScanOptions {
	return &ScanOptions{}
}

func Debug(enabled bool) ScanOption {
	return func(o *ScanOptions) {
		o.Debug = enabled
	}
}

type ScanOption func(o *ScanOptions)

func ScanStdin(ctx context.Context, opts ...ScanOption) error {
	return DebugPrintReader(ctx, os.Stdin, opts...)
}

func ScanFile(ctx context.Context, filename string, opts ...ScanOption) error {
	jsonFile, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer jsonFile.Close()
	return DebugPrintReader(ctx, jsonFile, opts...)
}

func fixOutputFilename(filename string) string {
	if !strings.HasSuffix(filename, ".kite") {
		log.Info().Str("filename", filename).Msg(".kite extension added to filename")
		return fmt.Sprintf("%s.kite", filename)
	}
	return filename
}

func CompileFile(ctx context.Context, input string, outputFile string, opts ...ScanOption) error {
	jsonFile, err := os.Open(input)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer jsonFile.Close()
	return Compile(ctx, jsonFile, outputFile, opts...)
}

func Compile(ctx context.Context, r io.Reader, outputFile string, opts ...ScanOption) error {
	// add the .kite extension if it doesnt exist
	outputFile = fixOutputFilename(outputFile)

	// stream the schema one api at a time and keep only the converted routes, the raw json of a large
	// dataset does not fit in memory
	var (
		merr       *multierror.Error
		apis       []proute.API
		withIssues int
	)
	err := kitebuilder.StreamJSON(r, func(api kitebuilder.API) error {
		a, err := proute.FromKitebuilderAPI(api)
		if errors.As(err, &merr) {
			// problems are per parameter (unknown formats, unsupported regexes, ...); the rest of the api is
			// still compiled. details at debug level, a summary at the end
			withIssues++
			log.Debug().Str("id", api.ID).Msg("problems while parsing api")
			for _, v := range merr.Errors {
				errors2.PrintError(v, 0)
			}
		} else if err != nil {
			return fmt.Errorf("failed to parse api %s: %w", api.ID, err)
		}
		apis = append(apis, a)
		return nil
	})
	if withIssues > 0 {
		log.Warn().Int("apis", len(apis)).Int("with-problems", withIssues).
			Msg("some parameters could not be fully parsed and use generic values (run with -v debug for details)")
	}
	if errors.As(err, &merr) {
		for _, v := range merr.Errors {
			errors2.PrintError(v, 0)
		}
	} else if err != nil {
		return fmt.Errorf("failed to parse json: %w", err)
	}

	if err := proute.APIS(apis).EncodeProtoFile(outputFile); err != nil {
		return fmt.Errorf("failed to encode apis: %w", err)
	}

	return nil
}

func DebugPrintReader(ctx context.Context, r io.Reader, opts ...ScanOption) error {
	options := NewDefaultScanOptions()
	for _, o := range opts {
		o(options)
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	if options.Debug {
		return DebugPrintBytes(data)
	} else {
		return PrintBytes(data)
	}
}

func DebugPrintBytes(data []byte) error {
	log.Debug().Msg("debug printing")

	var merr *multierror.Error
	api, err := kitebuilder.SlowLoadJSONBytes(data)
	if errors.As(err, &merr) {
		for _, v := range merr.Errors {
			errors2.PrintError(v, 0)
		}
	} else if err != nil {
		return fmt.Errorf("failed to parse json: %w", err)
	}

	// kitebuilder.PrintAPIs(api)
	// for _, v := range api {
	//	pr := proute.FromKitebuilderAPI(v)
	//	pr.DebugPrint()
	// }
	for _, v := range api {
		tmp, err := proute.FromKitebuilderAPI(v)
		if errors.As(err, &merr) {
			log.Error().Str("id", v.ID).Msg("errors while parsing api")
			for _, v := range merr.Errors {
				errors2.PrintError(v, 1)
			}
		} else if err != nil {
			return fmt.Errorf("failed to parse api: %w", err)
		}

		wcr, err := proute.ToKiterunnerRoutes(tmp)
		if errors.As(err, &merr) {
			log.Error().Str("id", v.ID).Msg("errors while building routes")
			for _, v := range merr.Errors {
				errors2.PrintError(v, 1)
			}
		} else if err != nil {
			return fmt.Errorf("failed to parse api: %w", err)
		}
		for _, v := range wcr {
			_ = v
			// log.Log().Object("route", v)
		}
	}
	return nil
}

func PrintBytes(data []byte) error {
	api, err := kitebuilder.LoadJSONBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse json: %w", err)
	}

	kitebuilder.PrintAPIs(api)
	return nil
}
