package util

import "encoding/json"

func RemoveNullFieldsFromJSON(jsonStr string) (string, error) {
	var data map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &data)
	if err != nil {
		return "", err
	}

	removeNullFieldsRecursive(data)

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}

func removeNullFieldsRecursive(data map[string]interface{}) {
	for k, v := range data {
		switch v := v.(type) {
		case map[string]interface{}:
			removeNullFieldsRecursive(v)
		case []interface{}:
			for _, item := range v {
				if f := item.(map[string]interface{}); f != nil {
					removeNullFieldsRecursive(f)
				}
			}
		case nil:
			delete(data, k)
		}
	}
}
