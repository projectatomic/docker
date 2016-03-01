// Main package for forward-journald
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/coreos/go-systemd/journal"
)

var pgrep_filter = flag.String("filter", "", "pgrep filter to determine process to forward")
var pri_info = flag.Bool("1", true, "forward stdin to journald as Priority Informational")
var pri_error = flag.Bool("2", false, "forward stdin to journald as Priority Error")

func init() {
	flag.StringVar(pgrep_filter, "f", "", "pgrep filter to determine process to forward")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %v:\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %v --filter docker\n\n", path.Base(os.Args[0]))
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if *pgrep_filter == "" {
		fmt.Fprintln(os.Stderr, "-filter option required.")
		flag.Usage()
		os.Exit(1)
	}

	forwarded_pid, err := GetProcess(*pgrep_filter)
	if forwarded_pid == "" {
		fmt.Fprintln(os.Stderr, "unable to find process from pgrep filter: %v", *pgrep_filter)
		flag.Usage()
		os.Exit(2)
	}

	if journal.Enabled() {
		journal.Send(fmt.Sprintf("Forwarding %v[%v] output to journald", *pgrep_filter, forwarded_pid), journal.PriInfo, nil)
	} else {
		fmt.Fprintln(os.Stderr, "forward-journald: Unable to connect to journald")
		os.Exit(3)
	}

	var fields map[string]string = nil

	if len(forwarded_pid) > 0 {
		fields = map[string]string{
			"OBJECT_PID": forwarded_pid,
		}
	}

	reader := bufio.NewReader(os.Stdin)

	var priority journal.Priority
	if *pri_error {
		priority = journal.PriErr
	} else {
		priority = journal.PriInfo
	}

	var line string = ""
	for err == nil {
		line, err = reader.ReadString('\n')

		if journal.Enabled() {
			if err == nil {
				line = strings.TrimSpace(line)
				journal.Send(line, priority, fields)
			} else {
				// log any partial lines
				if len(line) > 0 {
					journal.Send(line, priority, fields)
				}
			}
		}
	}
}
