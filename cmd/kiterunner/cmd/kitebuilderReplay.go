package cmd

import (
	"regexp"

	"github.com/assetnote/kiterunner/internal/kitebuilder"
	"github.com/assetnote/kiterunner/pkg/context"
	"github.com/assetnote/kiterunner/pkg/log"
	"github.com/spf13/cobra"
)

var (
	kitebuilderFile = ""
	proxy = ""
)

// replayCmd represents the replay command
var replayCmd = &cobra.Command{
	Use:   `replay "GET [   5,   2,   1] /foo -> /bar 0cc39f80913b550423454792b47ba661e6724a59" -w routes.kite`,
	Short: "replay a kitebuilder request based on the input",
	Long: `replay an kitebuilder request based on the input

supply the input raw, and we'll figure it out for you
or 
kr kb replay -w routes.kite <id> <method> <route> <host>
kr kb replay -w routes.kite "<full line output>"

e.g.
kr kb replay -w routes.kite 0cc39f80913b550423454792b47ba661e6724a59 GET /foo
kr kb replay -w routes.kite "POST    400 [    138,    5,  11] https://ap-service-team-hm.services.atlassian.com/volumes/create 0cc39f830ee6b0093e824073fd086bbd7c34b631"
kr kb replay -w routes.kite --proxy http://localhost:8080 "POST    403 [    126,   25,   6] https://artifactory.services.atlassian.com/REPORT_AVAILABLE 0cc39f84f1c74d86ceb7727823cd1cc0f996ea19" 
`,
	Args: cobra.MaximumNArgs(4),
	Run: func(cmd *cobra.Command, args []string) {
		var (
			ksuid string
			method string
			path string
			host string
		)

		switch len(args) {
		case 1:
			rg := regexp.MustCompile(`(\w+).*\[.*\] (https?://[^/]+)([^ ]+).* (.*)$`)
			matches := rg.FindStringSubmatch(args[0])
			if matches == nil {
				log.Fatal().Str("input", args[0]).Msg("failed to parse result line. expected output from kr scan, e.g. \"POST    400 [    138,    5,  11] https://host/path 0cc39f...\"")
			}
			ksuid = matches[4]
			method = matches[1]
			path = matches[3]
			host = matches[2]
		case 3, 4:
			// <ksuid> <method> <route> [host]. without a host the request is only printed, not sent
			ksuid = args[0]
			method = args[1]
			path = args[2]
			if len(args) == 4 {
				host = args[3]
			}
		default:
			log.Fatal().Int("args", len(args)).Msg("expected either a full result line, or <ksuid> <method> <route> [host]")
		}

		if err := kitebuilder.Replay(context.Context(), kitebuilderFile, ksuid, method, path, host, proxy); err != nil {
			log.Fatal().Err(err).Msg("failed to replay request")
		}
	},
}

func init() {
	kidebuilderCmd.AddCommand(replayCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// replayCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	replayCmd.Flags().StringVarP(&kitebuilderFile, "kitebuilder-list", "w", kitebuilderFile, "ogl wordlist to use for scanning")
	replayCmd.Flags().StringVarP(&proxy, "proxy", "p", proxy, "proxy to replay the request through")
}
