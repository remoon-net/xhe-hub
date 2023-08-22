package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/lainio/err2/try"
	"github.com/spf13/cobra"
)

// checkCmd represents the check command
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		addr := try.To1(os.ReadFile(listenAddrSavedFile))
		link := fmt.Sprintf("http://%s/health", string(addr))
		resp := try.To1(http.Get(link))
		if !strings.HasPrefix(resp.Status, "2") {
			body := try.To1(io.ReadAll(resp.Body))
			slog.Error("healthcheck",
				slog.String("status", resp.Status),
				slog.String("body", string(body)),
			)
			os.Exit(1)
			return
		}
	},
}

func init() {
	rootCmd.AddCommand(checkCmd)
}
