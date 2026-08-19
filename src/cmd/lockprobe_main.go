package main

import (
	"fmt"
	"os"
	"time"

	"github.com/termux-nas/nas/internal/mgmt"
)

func main() {
	path := "G:/tmp/nas-m6/run/nas.lock"
	for i := 0; i < 5; i++ {
		release, err := mgmt.AcquireLock(path)
		if err != nil {
			fmt.Printf("[%d] AcquireLock FAIL: %v\n", i, err)
		} else {
			fmt.Printf("[%d] AcquireLock OK, releasing\n", i)
			_ = release()
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = os.Args
}
