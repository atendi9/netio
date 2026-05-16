package netio

import (
	"log"
	"os"
	"strings"
)

type Logger func(msg ...string)

const disableFlags int = 0

func NewDefaultLogger(appName string) Logger {
	logger := log.New(os.Stdout, appName+" ▷ \t", disableFlags)

	return func(msgs ...string) {
		message := strings.Join(msgs, "")
		message = strings.ReplaceAll(message, "\n", "\n\t\t")

		logger.Println(message)
	}
}

// log writes through the app logger. New always assigns a logger, so a nil
// check is unnecessary here.
func (a *App) log(msgs ...string) {
	a.logger(msgs...)
}
