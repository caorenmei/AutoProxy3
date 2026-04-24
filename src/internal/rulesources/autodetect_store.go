package rulesources

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/caorenmei/autoproxy3/src/internal/rules"
)

// AutoDetectStore 表示 auto-detect 主机规则的文件持久化存储。
//
// 调用方通过 Path 指定落盘文件位置，并通过 AppendHost 以一行一个主机的格式追加新规则。
// 该类型会忽略空主机，并在文件中已存在相同主机时避免重复写入。
type AutoDetectStore struct {
	Path string
}

// AppendHost 将单个主机追加到 auto-detect 规则文件。
//
// 参数 host 为待持久化的主机名；函数会执行规范化并在输入为空时直接返回 nil。
// 当文件中已存在相同主机时不会重复写入；新主机会按一行一个主机的格式追加到 Path 指定文件。
func (s AutoDetectStore) AppendHost(host string) error {
	normalized := rules.NormalizeAutoDetectHost(host)
	if normalized == "" {
		return nil
	}

	existing, _ := os.ReadFile(s.Path)
	if containsStoredHost(existing, normalized) {
		return nil
	}

	file, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open auto-detect rules file: %w", err)
	}
	defer file.Close()

	_, err = fmt.Fprintln(file, normalized)
	return err
}

func containsStoredHost(content []byte, host string) bool {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		if rules.NormalizeAutoDetectHost(scanner.Text()) == host {
			return true
		}
	}
	return false
}
