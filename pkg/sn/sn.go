package sn

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/omalloc/contrib/v2/machineid"
)

const defaultWorkdir = "/var/lib/probecube-agent"

func snFilePath(workdir string) string {
	if workdir == "" {
		workdir = defaultWorkdir
	}
	return filepath.Join(workdir, "machine-id")
}

// Load reads the persistent serial number or derives and saves it in workdir.
// An empty workdir uses the default agent state directory.
func Load(workdir string) string {
	path := snFilePath(workdir)
	buf, err := os.ReadFile(path)
	if err != nil {
		machine := machineid.Load()
		sn := machine.SN()

		// 将 SN 写入文件，供后续读取
		err = os.MkdirAll(filepath.Dir(path), 0755)
		if err == nil {
			err = os.WriteFile(path, []byte(sn), 0644)
		}
		if err != nil {
			// 如果写入失败，返回 SN 但不阻止程序运行
			log.Warn("failed to write SN to file", "err", err)
		}
		return sn
	}

	return strings.TrimSpace(string(buf))
}
