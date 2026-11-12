package scan

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/vbauerster/mpb/v6"
)

type ProgressBar struct {
	Pb       *mpb.Progress
	Requests *progressbar.ProgressBar

	// progressbar does not synchronise ChangeMax64 with Add64, and the engine calls Incr and
	// AddTotal from different goroutines
	mu sync.Mutex
}

func NewProgress(max int64) *ProgressBar {
	pb := mpb.New(
		mpb.WithOutput(os.Stderr),
	)
	requestb := progressbar.NewOptions64(max,
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(5),
		progressbar.OptionShowIts(),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
		progressbar.OptionSetVisibility(true),
		progressbar.OptionSpinnerType(14),
		// progressbar.OptionFullWidth(),
	)
	return &ProgressBar{
		Pb:       pb,
		Requests: requestb,
	}
}

func (b *ProgressBar) Incr(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Requests.Add64(n)
}

func (b *ProgressBar) AddTotal(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Requests.ChangeMax64(b.Requests.GetMax64() + n)
}
