package netio

import (
	"fmt"
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

func (a *App) log(msgs ...string) {
	if a.logger != nil {
		a.logger(msgs...)
		return
	}
	message := strings.Join(msgs, "")
	fmt.Print(a.newMsg(message))
}

func (a *App) newMsg(msg string) string {
	return fmt.Sprintf("\r%s ▷ %s\n", a.appName, msg)
}
