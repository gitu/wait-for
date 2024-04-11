package cmd

import (
	"fmt"
	"github.com/gitu/wait-for/pkg/wait"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var w wait.Wait

var rootCmd = &cobra.Command{
	Use:   "wait-for",
	Short: "wait-for is a small command-line utility to wait for a service to become available.",
	Long: `Simple utility to wait for a service to become available.  It accepts a list of hosts and ports and waits
until one or all of them is available or a timeout occurs. It can also wait for an expected http status code.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := w.Wait(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringSliceVarP(&w.HostPorts, "host-port", "p", []string{}, "host:port to wait for")
	rootCmd.Flags().StringSliceVarP(&w.Urls, "url", "u", []string{}, "urls to wait for (http://host:port/path) can be prefixed with expected "+
		"status code (e.g. 201:http://host/path) uses 200 as default")
	rootCmd.Flags().DurationVarP(&w.GlobalTimeout, "timeout", "t", 30*time.Second, "global timeout for all urls")
	rootCmd.Flags().DurationVarP(&w.CheckTimeout, "checkTimeout", "e", 1*time.Second, "individual check timeout for each individual check")
	rootCmd.Flags().DurationVarP(&w.StatusInterval, "statusInterval", "i", 10*time.Second, "interval to current status of urls")
	rootCmd.Flags().DurationVarP(&w.CheckInterval, "checkInterval", "c", 500*time.Millisecond, "interval to wait between checks")
	rootCmd.MarkFlagsOneRequired("host-port", "url")
}
