package draftkings

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

func decodeDraftKingsSocketFrame(raw []byte) (draftKingsSocketFrame, error) {
	var envelope []any
	if err := msgpack.Unmarshal(raw, &envelope); err != nil {
		return draftKingsSocketFrame{}, err
	}
	if len(envelope) < 2 {
		return draftKingsSocketFrame{}, errUnsupportedDelta
	}
	id, idOK := envelope[0].(string)
	kind, kindOK := envelope[1].(string)
	if !kindOK || (!idOK && !(envelope[0] == nil && (kind == "error" || kind == "unsubscribed"))) {
		return draftKingsSocketFrame{}, errUnsupportedDelta
	}
	frame := draftKingsSocketFrame{id: id, kind: kind}
	if kind != "update" {
		if kind == "error" || kind == "unsubscribed" {
			frame.failureDetail = "reason=not provided"
			for _, part := range envelope[2:] {
				if detail := subscriptionFailureDetail(part); detail != "reason=not provided" {
					frame.failureDetail = detail
					break
				}
			}
		}
		return frame, nil
	}
	if len(envelope) < 3 {
		return frame, errUnsupportedDelta
	}
	payload, ok := envelope[2].([]any)
	if !ok || len(payload) < 1 {
		return frame, errUnsupportedDelta
	}
	groups, ok := payload[0].([]any)
	if !ok || len(groups) != 3 {
		return frame, errUnsupportedDelta
	}
	var sourcePublishedAt time.Time
	if len(payload) > 2 {
		if metadata, ok := payload[2].(map[string]any); ok {
			if published, ok := metadata["publishedTime"].(string); ok {
				sourcePublishedAt, _ = time.Parse(time.RFC3339Nano, published)
			}
		}
	}
	for groupIndex, group := range groups {
		sections, ok := group.([]any)
		if !ok || (groupIndex == 2 && len(sections) != 3) {
			return frame, fmt.Errorf("%w: group %d has %d sections", errUnsupportedDelta, groupIndex, len(sections))
		}
		for sectionIndex, section := range sections {
			entries, ok := section.([]any)
			if !ok {
				return frame, errUnsupportedDelta
			}
			for _, entry := range entries {
				switch {
				case groupIndex < 2 && sectionIndex > 2:
					frame.requiresResync = true
				case groupIndex < 2 && sectionIndex < 2:
					// A game or market was added/removed; REST must rebuild the join.
					frame.requiresResync = true
				case groupIndex < 2 && sectionIndex == 2:
					// New alternatives are not necessarily the current main line.
					// Opcode 24 identifies a later promotion to the main line.
					if groupIndex == 0 && !socketOperation(entry, 35) {
						frame.requiresResync = true
					}
					if groupIndex == 1 {
						if id, ok := entry.(string); ok {
							frame.removedSelectionIDs = append(frame.removedSelectionIDs, id)
						} else {
							frame.requiresResync = true
						}
					}
				case groupIndex == 2 && sectionIndex < 2:
					// Live score/event (22) and market metadata (23) do not
					// change the displayed quote on their own.
					if !socketOperation(entry, int64(22+sectionIndex)) {
						frame.requiresResync = true
					}
				default:
					selection, err := decodeLiveSelection(entry)
					if err != nil {
						return frame, err
					}
					selection.SourcePublishedAt = sourcePublishedAt
					frame.selections = append(frame.selections, selection)
				}
			}
		}
	}
	return frame, nil
}

func socketOperation(entry any, opcode int64) bool {
	pair, ok := entry.([]any)
	return ok && len(pair) == 2 && numericEquals(pair[0], opcode)
}

func decodeLiveSelection(entry any) (liveSelectionUpdate, error) {
	pair, ok := entry.([]any)
	if !ok || len(pair) != 2 || !numericEquals(pair[0], 24) {
		return liveSelectionUpdate{}, fmt.Errorf("%w: unknown selection operation", errUnsupportedDelta)
	}
	fields, ok := pair[1].([]any)
	if !ok || len(fields) != 8 {
		return liveSelectionUpdate{}, fmt.Errorf("%w: unexpected selection fields", errUnsupportedDelta)
	}
	replacesID := ""
	if fields[7] != nil {
		var ok bool
		replacesID, ok = fields[7].(string)
		if !ok || replacesID == "" {
			return liveSelectionUpdate{}, fmt.Errorf("%w: invalid replacement selection ID", errUnsupportedDelta)
		}
	}
	id, idOK := fields[0].(string)
	label, labelOK := fields[1].(string)
	odds, oddsOK := fields[2].([]any)
	if !idOK || !labelOK || !oddsOK || len(odds) < 2 {
		return liveSelectionUpdate{}, errUnsupportedDelta
	}
	american, americanOK := odds[0].(string)
	decimal, decimalOK := odds[1].(string)
	if !americanOK || !decimalOK || id == "" || label == "" || american == "" || decimal == "" {
		return liveSelectionUpdate{}, errUnsupportedDelta
	}
	price := Price{American: strings.ReplaceAll(american, "−", "-"), Decimal: decimal}
	if fields[4] != nil {
		point, ok := numberAsFloat(fields[4])
		if !ok || math.IsNaN(point) || math.IsInf(point, 0) {
			return liveSelectionUpdate{}, errUnsupportedDelta
		}
		price.Line = &point
	}
	return liveSelectionUpdate{ID: id, ReplacesID: replacesID, Label: label, Price: price}, nil
}

func numericEquals(value any, want int64) bool {
	switch number := value.(type) {
	case int8:
		return int64(number) == want
	case int16:
		return int64(number) == want
	case int32:
		return int64(number) == want
	case int64:
		return number == want
	case int:
		return int64(number) == want
	case uint8:
		return int64(number) == want
	case uint16:
		return int64(number) == want
	case uint32:
		return int64(number) == want
	case uint64:
		return number == uint64(want)
	default:
		return false
	}
}

func numberAsFloat(value any) (float64, bool) {
	switch number := value.(type) {
	case float32:
		return float64(number), true
	case float64:
		return number, true
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}
