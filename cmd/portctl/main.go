// Command portctl 是南海伏季休渔渔港调度平台的命令行入口。
package main

import (
	"os"

	"nanhaiport/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
