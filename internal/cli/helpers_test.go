package cli

import (
	"encoding/json"
	"os"
)

// readJSONFile 读取并解析测试过程中写出的 JSON 文件。
func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}
