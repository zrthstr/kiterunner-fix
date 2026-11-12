package cmd

import (
	"github.com/spf13/cobra"
	"github.com/zrthstr/kiterunner-fix/internal/wordlist"
	"github.com/zrthstr/kiterunner-fix/pkg/context"
	"github.com/zrthstr/kiterunner-fix/pkg/log"
)

var ()

// wordlistListCmd represents the wordlistList command
var wordlistListCmd = &cobra.Command{
	Use:   "list",
	Short: "list the wordlists cached and available",
	Long: `list will show all the remote wordlists and all the local wordlists
with the corresponding abbreviations for use
`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt, err := wordlist.FormatFromString(Output)
		if err != nil {
			log.Fatal().Err(err).Msg("invalid format")
		}
		if err := wordlist.List(context.Context(), wordlist.OutputFormat(fmt)); err != nil {
			log.Fatal().Err(err).Msg("failed to list wordlists")
		}
	},
}

func init() {
	wordlistCmd.AddCommand(wordlistListCmd)
}
