// Package tiktokenbpe 把 tiktoken 需要的 BPE 词表文件嵌入到二进制中，
// 避免容器运行时因无法访问 Azure Blob（https://openaipublic.blob.core.windows.net）
// 而在初始化 tokenizer 时无限卡住。
package tiktokenbpe

import (
	"embed"
	"encoding/base64"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

//go:embed *.tiktoken
var bpeFiles embed.FS

type embedBpeLoader struct{}

// LoadTiktokenBpe 根据 tiktokenBpeFile（实际为完整 URL）找到对应的嵌入文件并解析。
func (l *embedBpeLoader) LoadTiktokenBpe(tiktokenBpeFile string) (map[string]int, error) {
	name := path.Base(tiktokenBpeFile)
	data, err := bpeFiles.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("embedded bpe file %q not found: %w", name, err)
	}

	ranks := make(map[string]int)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			continue
		}
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, err
		}
		rank, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, err
		}
		ranks[string(token)] = rank
	}
	return ranks, nil
}

func init() {
	tiktoken.SetBpeLoader(&embedBpeLoader{})
}
