package stream

import (
	"encoding/binary"
	"fmt"
)

// EventStreamParser AWS Event Stream 二进制格式解析器
type EventStreamParser struct {
	buffer []byte
}

// Event 事件
type Event struct {
	Headers map[string]string
	Payload []byte
}

// NewEventStreamParser 创建事件流解析器
func NewEventStreamParser() *EventStreamParser {
	return &EventStreamParser{
		buffer: make([]byte, 0),
	}
}

// Feed 添加数据到缓冲区并返回完整的事件
func (p *EventStreamParser) Feed(data []byte) ([]Event, error) {
	p.buffer = append(p.buffer, data...)
	return p.parseEvents()
}

func (p *EventStreamParser) parseEvents() ([]Event, error) {
	var events []Event

	for {
		if len(p.buffer) < 12 {
			// 数据不足以解析前导
			break
		}

		// 读取总长度（大端序 uint32）
		totalLength := binary.BigEndian.Uint32(p.buffer[0:4])

		if uint32(len(p.buffer)) < totalLength {
			// 数据不足以解析完整消息
			break
		}

		// 解析单条消息
		event, err := p.parseMessage(p.buffer[:totalLength])
		if err != nil {
			return events, err
		}

		events = append(events, event)
		p.buffer = p.buffer[totalLength:]
	}

	return events, nil
}

func (p *EventStreamParser) parseMessage(data []byte) (Event, error) {
	if len(data) < 16 {
		return Event{}, fmt.Errorf("消息太短")
	}

	// 前导: total_length (4) + headers_length (4) + prelude_crc (4)
	headersLength := binary.BigEndian.Uint32(data[4:8])

	// 头部从偏移 12 开始
	headersEnd := 12 + headersLength
	if uint32(len(data)) < headersEnd+4 {
		return Event{}, fmt.Errorf("消息长度无效")
	}

	headers, err := p.parseHeaders(data[12:headersEnd])
	if err != nil {
		return Event{}, err
	}

	// 负载在头部和消息 CRC（最后 4 字节）之间
	payloadEnd := uint32(len(data)) - 4
	payload := data[headersEnd:payloadEnd]

	return Event{
		Headers: headers,
		Payload: payload,
	}, nil
}

func (p *EventStreamParser) parseHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	offset := 0

	for offset < len(data) {
		if offset+1 > len(data) {
			break
		}

		// 头部名称长度（1 字节）
		nameLen := int(data[offset])
		offset++

		if offset+nameLen > len(data) {
			return headers, fmt.Errorf("头部名称长度无效")
		}

		// 头部名称
		name := string(data[offset : offset+nameLen])
		offset += nameLen

		if offset+1 > len(data) {
			return headers, fmt.Errorf("缺少头部类型")
		}

		// 头部类型（1 字节）
		headerType := data[offset]
		offset++

		// 根据类型解析值
		var value string
		switch headerType {
		case 0: // bool true
			value = "true"
		case 1: // bool false
			value = "false"
		case 2: // byte
			if offset+1 > len(data) {
				return headers, fmt.Errorf("byte 头部无效")
			}
			value = fmt.Sprintf("%d", data[offset])
			offset++
		case 3: // short (2 字节)
			if offset+2 > len(data) {
				return headers, fmt.Errorf("short 头部无效")
			}
			value = fmt.Sprintf("%d", binary.BigEndian.Uint16(data[offset:offset+2]))
			offset += 2
		case 4: // int (4 字节)
			if offset+4 > len(data) {
				return headers, fmt.Errorf("int 头部无效")
			}
			value = fmt.Sprintf("%d", binary.BigEndian.Uint32(data[offset:offset+4]))
			offset += 4
		case 5: // long (8 字节)
			if offset+8 > len(data) {
				return headers, fmt.Errorf("long 头部无效")
			}
			value = fmt.Sprintf("%d", binary.BigEndian.Uint64(data[offset:offset+8]))
			offset += 8
		case 6: // bytes
			if offset+2 > len(data) {
				return headers, fmt.Errorf("bytes 头部长度无效")
			}
			bytesLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+bytesLen > len(data) {
				return headers, fmt.Errorf("bytes 头部值无效")
			}
			value = string(data[offset : offset+bytesLen])
			offset += bytesLen
		case 7: // string
			if offset+2 > len(data) {
				return headers, fmt.Errorf("string 头部长度无效")
			}
			strLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+strLen > len(data) {
				return headers, fmt.Errorf("string 头部值无效")
			}
			value = string(data[offset : offset+strLen])
			offset += strLen
		case 8: // timestamp (8 字节)
			if offset+8 > len(data) {
				return headers, fmt.Errorf("timestamp 头部无效")
			}
			value = fmt.Sprintf("%d", binary.BigEndian.Uint64(data[offset:offset+8]))
			offset += 8
		case 9: // UUID (16 字节)
			if offset+16 > len(data) {
				return headers, fmt.Errorf("UUID 头部无效")
			}
			value = fmt.Sprintf("%x", data[offset:offset+16])
			offset += 16
		default:
			return headers, fmt.Errorf("未知头部类型: %d", headerType)
		}

		headers[name] = value
	}

	return headers, nil
}

