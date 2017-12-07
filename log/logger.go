package log

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strings"
)

type Logger struct {
	Logger *log.Logger
	Name   string
}

var (
	// ioutil.Discard
	TraceLogger = Logger{Name: "TRACE"}
	DebugLogger = Logger{Name: "DEBUG"}
	InfoLogger  = Logger{Name: "INFO"}
	WarnLogger  = Logger{Name: "WARN"}
	ErrorLogger = Logger{Name: "ERROR"}
	FatalLogger = Logger{Name: "FATAL"}

	ProgramName string
	loggers     = []*Logger{&TraceLogger, &DebugLogger, &InfoLogger, &WarnLogger, &ErrorLogger, &FatalLogger}
	callDepth   = 2
)

func init() {
	ProgramName = path.Base(os.Args[0])

	initForProduction("WARN")
}

func InitFromEnvironment(envName string) {
	lev := strings.ToUpper(os.Getenv(envName))

	if lev == "" {
		initForProduction("WARN")
	} else {
		initForDebugging(lev)
	}
}

func initForDebugging(level string) {
	var writer io.Writer = ioutil.Discard
	for i := 0; i < len(loggers); i++ {
		if level == loggers[i].Name {
			writer = os.Stderr
		}
		loggers[i].Logger = log.New(writer, fmt.Sprintf("# %s: ", loggers[i].Name), log.Lshortfile)
	}
}

func initForProduction(level string) {
	var writer io.Writer = ioutil.Discard
	for i := 0; i < len(loggers); i++ {
		if level == loggers[i].Name {
			writer = os.Stderr
		}
		loggers[i].Logger = log.New(writer, fmt.Sprintf("%s:", ProgramName), log.Lshortfile)
	}
}

func Trace(format string, v ...interface{}) {
	TraceLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
}

func Debug(format string, v ...interface{}) {
	DebugLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
}

func Info(format string, v ...interface{}) {
	InfoLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
}

func Warn(format string, v ...interface{}) {
	WarnLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
}

func Err(format string, v ...interface{}) {
	ErrorLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
}

func ErrQuit(exitCode int, format string, v ...interface{}) {
	ErrorLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func Fatal(format string, v ...interface{}) {
	FatalLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
	os.Exit(1)
}

func FatalQuit(exitCode int, format string, v ...interface{}) {
	FatalLogger.Logger.Output(callDepth, fmt.Sprintf(format, v...))
	os.Exit(exitCode)
}

func main() {
	InitFromEnvironment("LOG")
	Trace("this is TRACE log")
	Debug("this is DEBUG log")
	Info("this is INFO log")
	Warn("this is WARN log")
	Err("this is ERROR log")
	Fatal("this is FATAL log")
	Err("this is error log, but never reach here")
}
