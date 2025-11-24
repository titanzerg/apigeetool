package util

import (
	"sort"
	"strings"
)

// TrimStrings removes whitespace entries and trims each value.
func TrimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// UniqueSortedStrings deduplicates case-insensitively and sorts results.
func UniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		key := strings.ToLower(strings.TrimSpace(v))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(v))
	}
	sort.Strings(out)
	return out
}

// MergeAndUnique appends extra onto base, removing duplicates case-insensitively while preserving order.
func MergeAndUnique(base, extra []string) []string {
	result := append([]string{}, TrimStrings(base)...)
	seen := make(map[string]struct{}, len(result))
	for _, val := range result {
		seen[strings.ToLower(val)] = struct{}{}
	}
	for _, val := range extra {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		key := strings.ToLower(val)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, val)
	}
	return result
}

// UniqueStrings preserves order, trimming and deduplicating case-insensitively.
func UniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
