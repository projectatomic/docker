package image

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"text/tabwriter"

	"golang.org/x/net/context"

	"github.com/docker/docker/api/client"
	"github.com/docker/docker/cli"
	"github.com/docker/docker/pkg/stringutils"
	"github.com/docker/docker/registry"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
	"github.com/spf13/cobra"
)

type searchOptions struct {
	term    string
	noTrunc bool
	noIndex bool
	limit   int
	filter  []string

	// Deprecated
	stars     uint
	automated bool
}

// NewSearchCommand create a new `docker search` command
func NewSearchCommand(dockerCli *client.DockerCli) *cobra.Command {
	var opts searchOptions

	cmd := &cobra.Command{
		Use:   "search [OPTIONS] TERM",
		Short: "Search the Docker Hub for images",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.term = args[0]
			return runSearch(dockerCli, opts)
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&opts.noTrunc, "no-trunc", false, "Don't truncate output")
	flags.BoolVar(&opts.noTrunc, "no-index", false, "Don't truncate output")
	flags.StringSliceVarP(&opts.filter, "filter", "f", []string{}, "Filter output based on conditions provided")
	flags.IntVar(&opts.limit, "limit", registry.DefaultSearchLimit, "Max number of search results")

	flags.BoolVar(&opts.automated, "automated", false, "Only show automated builds")
	flags.UintVarP(&opts.stars, "stars", "s", 0, "Only displays with at least x stars")

	flags.MarkDeprecated("automated", "use --filter=automated=true instead")
	flags.MarkDeprecated("stars", "use --filter=stars=3 instead")

	return cmd
}

func runSearch(dockerCli *client.DockerCli, opts searchOptions) error {
	indexInfo, err := registry.ParseSearchIndexInfo(opts.term)
	if err != nil {
		return err
	}

	ctx := context.Background()

	requestPrivilege := dockerCli.RegistryAuthenticationPrivilegedFunc(indexInfo, "search", false)

	encodedAuth, err := client.EncodeAuthToBase64(dockerCli.ConfigFile().AuthConfigs)
	if err != nil {
		return err
	}

	searchFilters := filters.NewArgs()
	for _, f := range opts.filter {
		var err error
		searchFilters, err = filters.ParseFlag(f, searchFilters)
		if err != nil {
			return err
		}
	}

	options := types.ImageSearchOptions{
		RegistryAuth:  encodedAuth,
		NoIndex:       opts.noIndex,
		PrivilegeFunc: requestPrivilege,
		Filters:       searchFilters,
		Limit:         opts.limit,
	}

	clnt := dockerCli.Client()
	results, err := clnt.ImageSearch(ctx, opts.term, options)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(dockerCli.Out(), 10, 1, 3, ' ', 0)
	if opts.noIndex {
		fmt.Fprintf(w, "NAME\tDESCRIPTION\tSTARS\tOFFICIAL\tAUTOMATED\n")
	} else {
		fmt.Fprintf(w, "INDEX\tNAME\tDESCRIPTION\tSTARS\tOFFICIAL\tAUTOMATED\n")
	}
	for _, res := range results {
		// --automated and -s, --stars are deprecated since Docker 1.12
		if (opts.automated && !res.IsAutomated) || (int(opts.stars) > res.StarCount) {
			continue
		}
		row := []string{}
		if !opts.noIndex {
			indexName := res.IndexName
			if !opts.noTrunc {
				// Shorten index name to DOMAIN.TLD unless --no-trunc is given.
				if host, _, err := net.SplitHostPort(indexName); err == nil {
					indexName = host
				}
				// do not shorten ip address
				if net.ParseIP(indexName) == nil {
					// shorten index name just to the last 2 components (`DOMAIN.TLD`)
					indexNameSubStrings := strings.Split(indexName, ".")
					if len(indexNameSubStrings) > 2 {
						indexName = strings.Join(indexNameSubStrings[len(indexNameSubStrings)-2:], ".")
					}
				}
			}
			row = append(row, indexName)
		}

		desc := strings.Replace(res.Description, "\n", " ", -1)
		desc = strings.Replace(desc, "\r", " ", -1)
		if !opts.noTrunc && len(desc) > 45 {
			desc = stringutils.Truncate(desc, 42) + "..."
		}
		row = append(row, res.RegistryName+"/"+res.Name, desc, strconv.Itoa(res.StarCount), "", "")
		if res.IsOfficial {
			row[len(row)-2] = "[OK]"
		}
		if res.IsAutomated {
			row[len(row)-1] = "[OK]"
		}
		fmt.Fprintf(w, "%s\n", strings.Join(row, "\t"))
	}
	w.Flush()
	return nil
}
