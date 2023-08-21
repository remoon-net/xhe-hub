package hub

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/lainio/err2/try"
)

func sendComment(w *echo.Response) {
	defer w.Flush()
	try.To1(fmt.Fprintln(w, ": a hack comment for pass caddy"))
	try.To1(fmt.Fprintln(w, ""))
}

func writeHeader(w *echo.Response) {
	defer w.Flush()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
}

type Event struct {
	ID   string
	Data []byte
	Name string
}

var _ fmt.Stringer = (*Event)(nil)

var dataReplacer = strings.NewReplacer(
	"\n", "\ndata:",
	"\r", "\\r",
)

func (ev *Event) String() string {
	var w = bytes.NewBufferString("")
	fmt.Fprintln(w, "id:"+ev.ID)
	if len(ev.Name) > 0 {
		fmt.Fprintln(w, "event:"+ev.Name)
	}
	w.WriteString("data:")
	dataReplacer.WriteString(w, string(ev.Data))
	w.WriteString("\n")
	return w.String()
}

func parseUnixTimestamp(str string) (t time.Time) {
	i, _ := strconv.ParseInt(str, 10, 64)
	t = time.Unix(i, 0)
	return t
}
