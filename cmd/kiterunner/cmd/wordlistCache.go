package cmd

import (
	"github.com/spf13/cobra"
	"github.com/zrthstr/kiterunner-fix/internal/wordlist"
	"github.com/zrthstr/kiterunner-fix/pkg/context"
	"github.com/zrthstr/kiterunner-fix/pkg/log"
)

var (
	noCache bool = false
)

// wordlistSaveCmd represents the wordlistCache command
var wordlistSaveCmd = &cobra.Command{
	Use:   "save [wordlists ...]",
	Short: "save the wordlists specified (full filename or alias)",
	Long: `save will download the wordlists specified to ~/.cache/kiterunner/wordlists

you can use the alias or the full filename listed in [kr wordlist list]
`,

	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if _, err := wordlist.Save(context.Context(), noCache, args...); err != nil {
			log.Fatal().Err(err).Msg("failed to list wordlists")
		}
	},
}

func init() {
	wordlistCmd.AddCommand(wordlistSaveCmd)
	wordlistSaveCmd.Flags().BoolVar(&noCache, "no-cache", noCache, "delete the local files matching the names and pull fresh files")
}
