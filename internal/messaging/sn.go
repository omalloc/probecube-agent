package messaging

import (
	"os"
	"strings"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/omalloc/contrib/v2/machineid"
)

const SNFilePath = "/var/lib/probecube-agent/machine-id"

func loadSN() string {
	buf, err := os.ReadFile(SNFilePath)
	if err != nil {
		machine := machineid.Load()
		sn := machine.SN()

		// 将 SN 写入文件，供后续读取
		err = os.WriteFile(SNFilePath, []byte(sn), 0644)
		if err != nil {
			// 如果写入失败，返回 SN 但不阻止程序运行
			log.Warn("failed to write SN to file", "err", err)
		}
		return sn
	}

	return strings.TrimSpace(string(buf))
}
