package distribution

import (
	"fmt"
	"strings"
)

func combineErrors(errors ...error) error {
	if len(errors) == 0 {
		return nil
	}
	if len(errors) == 1 {
		return errors[0]
	}
	msgs := []string{}
	for _, err := range errors {
		msgs = append(msgs, err.Error())
	}
	return fmt.Errorf(strings.Join(msgs, "\n"))
}
