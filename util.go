package bsck

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"

	"github.com/codingeasygo/util/xio/frame"
)

func writeCmd(w frame.Writer, buffer []byte, cmd byte, sid uint64, msg []byte) (err error) {
	if buffer == nil {
		buffer = make([]byte, len(msg)+13)
	}
	buffer[4] = cmd
	binary.BigEndian.PutUint64(buffer[5:], sid)
	copy(buffer[13:], msg)
	_, err = w.WriteFrame(buffer[:len(msg)+13])
	return
}

//LogLevel is log leveo config
var LogLevel int = 3

//Log is the bsck package default log
var Log = log.New(os.Stdout, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)

//DebugLog is log by debug level
func DebugLog(format string, args ...interface{}) {
	if LogLevel >= 3 {
		Log.Output(2, fmt.Sprintf("D "+format, args...))
	}
}

//InfoLog is log by info level
func InfoLog(format string, args ...interface{}) {
	if LogLevel >= 2 {
		Log.Output(2, fmt.Sprintf("I "+format, args...))
	}
}

//WarnLog is log by warn level
func WarnLog(format string, args ...interface{}) {
	if LogLevel >= 1 {
		Log.Output(2, fmt.Sprintf("W "+format, args...))
	}
}

//ErrorLog is log by error level
func ErrorLog(format string, args ...interface{}) {
	if LogLevel >= 0 {
		Log.Output(2, fmt.Sprintf("E "+format, args...))
	}
}
