package amazonq

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// EventStreamParser 解析 AWS Event Stream 二进制格式
type EventStreamParser struct {
	buffer *bytes.Buffer
}

// NewEventStreamParser 创建新的解析器
func NewEventStreamParser() *EventStreamParser {
	return &EventStreamParser{
		buffer: new(bytes.Buffer),
	}
}

// Feed 向解析器缓冲区添加数据
func (p *EventStreamParser) Feed(data []byte) ([]Event, error) {
	p.buffer.Write(data)
	return p.parseEvents()
}

// Event 表示解析后的事件
type Event struct {
	Headers map[string]interface{}
	Payload []byte
}

func (p *EventStreamParser) parseEvents() ([]Event, error) {
	events := []Event{}

	for {
		if p.buffer.Len() < 12 {
			break
		}

		// 读取前导（12 字节）
		preludeBytes := p.buffer.Bytes()[:12]
		totalLength := binary.BigEndian.Uint32(preludeBytes[0:4])
		headersLength := binary.BigEndian.Uint32(preludeBytes[4:8])

		if totalLength < 16 {
			// 无效消息，跳过一个字节
			p.buffer.Next(1)
			continue
		}

		if uint32(p.buffer.Len()) < totalLength {
			break
		}

		// 读取完整消息
		messageBytes := p.buffer.Next(int(totalLength))

		// 解析头部
		headersData := messageBytes[12 : 12+headersLength]
		headers := parseHeaders(headersData)

		// 解析 payload
		payloadStart := 12 + headersLength
		payloadEnd := totalLength - 4 // 排除消息 CRC
		payload := messageBytes[payloadStart:payloadEnd]

		events = append(events, Event{
			Headers: headers,
			Payload: payload,
		})
	}

	return events, nil
}

func parseHeaders(data []byte) map[string]interface{} {
	headers := make(map[string]interface{})
	offset := 0

	for offset < len(data) {
		if offset >= len(data) {
			break
		}

		// 头部名称长度
		nameLength := int(data[offset])
		offset++

		if offset+nameLength > len(data) {
			break
		}

		// 头部名称
		name := string(data[offset : offset+nameLength])
		offset += nameLength

		if offset >= len(data) {
			break
		}

		// 头部值类型
		valueType := data[offset]
		offset++

		// 根据类型解析值
		var value interface{}
		switch valueType {
		case 0: // True
			value = true
		case 1: // False
			value = false
		case 2: // Byte
			if offset+1 > len(data) {
				break
			}
			value = data[offset]
			offset++
		case 3: // Short
			if offset+2 > len(data) {
				break
			}
			value = int16(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
		case 4: // Integer
			if offset+4 > len(data) {
				break
			}
			value = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
			offset += 4
		case 5: // Long
			if offset+8 > len(data) {
				break
			}
			value = int64(binary.BigEndian.Uint64(data[offset : offset+8]))
			offset += 8
		case 6: // Bytes
			if offset+2 > len(data) {
				break
			}
			length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+length > len(data) {
				break
			}
			value = data[offset : offset+length]
			offset += length
		case 7: // String
			if offset+2 > len(data) {
				break
			}
			length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+length > len(data) {
				break
			}
			value = string(data[offset : offset+length])
			offset += length
		case 8: // Timestamp
			if offset+8 > len(data) {
				break
			}
			value = binary.BigEndian.Uint64(data[offset : offset+8])
			offset += 8
		case 9: // UUID
			if offset+16 > len(data) {
				break
			}
			value = fmt.Sprintf("%x-%x-%x-%x-%x",
				data[offset:offset+4],
				data[offset+4:offset+6],
				data[offset+6:offset+8],
				data[offset+8:offset+10],
				data[offset+10:offset+16])
			offset += 16
		default:
			break
		}

		headers[name] = value
	}

	return headers
}

// ParsePayload 解析 JSON payload
func ParsePayload(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ExtractEventType 从头部提取事件类型
func ExtractEventType(headers map[string]interface{}) string {
	if eventType, ok := headers[":event-type"].(string); ok {
		return eventType
	}
	if eventType, ok := headers["event-type"].(string); ok {
		return eventType
	}
	return ""
}

