package pii

import (
	"strconv"
	"strings"
	"time"
)

var validProvinceCodes = map[string]bool{
	"11": true, "12": true, "13": true, "14": true, "15": true,
	"21": true, "22": true, "23": true,
	"31": true, "32": true, "33": true, "34": true, "35": true, "36": true, "37": true,
	"41": true, "42": true, "43": true, "44": true, "45": true, "46": true,
	"50": true, "51": true, "52": true, "53": true, "54": true,
	"61": true, "62": true, "63": true, "64": true, "65": true,
	"71": true, "81": true, "82": true,
}

func validResidentID(value string) bool {
	if len(value) != 18 || !validProvinceCodes[value[:2]] || value[2:6] == "0000" {
		return false
	}
	for i := 0; i < 17; i++ {
		if !asciiDigit(value[i]) {
			return false
		}
	}
	year, _ := strconv.Atoi(value[6:10])
	month, _ := strconv.Atoi(value[10:12])
	day, _ := strconv.Atoi(value[12:14])
	if year < 1800 || year > 2099 || month < 1 || month > 12 || day < 1 || day > 31 {
		return false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if date.Year() != year || int(date.Month()) != month || date.Day() != day {
		return false
	}
	weights := [...]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	checks := "10X98765432"
	sum := 0
	for i := 0; i < 17; i++ {
		sum += int(value[i]-'0') * weights[i]
	}
	actual := value[17]
	if actual == 'x' {
		actual = 'X'
	}
	return actual == checks[sum%11]
}

func validEmail(value string) bool {
	if len(value) > 254 || strings.Count(value, "@") != 1 {
		return false
	}
	parts := strings.SplitN(value, "@", 2)
	local, domain := parts[0], parts[1]
	if local == "" || len(local) > 64 || local[0] == '.' || local[len(local)-1] == '.' || strings.Contains(local, "..") {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			if !asciiDigit(label[i]) && !(label[i] >= 'A' && label[i] <= 'Z') &&
				!(label[i] >= 'a' && label[i] <= 'z') && label[i] != '-' {
				return false
			}
		}
	}
	return true
}

func validLuhn(value string) bool {
	sum := 0
	double := false
	for i := len(value) - 1; i >= 0; i-- {
		if !asciiDigit(value[i]) {
			return false
		}
		digit := int(value[i] - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return sum%10 == 0
}

var vinValues = map[byte]int{
	'A': 1, 'B': 2, 'C': 3, 'D': 4, 'E': 5, 'F': 6, 'G': 7, 'H': 8,
	'J': 1, 'K': 2, 'L': 3, 'M': 4, 'N': 5, 'P': 7, 'R': 9,
	'S': 2, 'T': 3, 'U': 4, 'V': 5, 'W': 6, 'X': 7, 'Y': 8, 'Z': 9,
}

func validVIN(value string) bool {
	if len(value) != 17 {
		return false
	}
	weights := [...]int{8, 7, 6, 5, 4, 3, 2, 10, 0, 9, 8, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i := 0; i < len(value); i++ {
		var v int
		if asciiDigit(value[i]) {
			v = int(value[i] - '0')
		} else {
			var ok bool
			v, ok = vinValues[value[i]]
			if !ok {
				return false
			}
		}
		sum += v * weights[i]
	}
	check := byte('0' + sum%11)
	if sum%11 == 10 {
		check = 'X'
	}
	return value[8] == check
}
