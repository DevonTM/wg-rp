package utils

import (
	"fmt"
	"time"
)

// FormatDuration formats a duration in a human-readable way
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "less than 1 second"
	}

	totalSeconds := int(d.Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60

	if minutes == 0 {
		if seconds == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", seconds)
	}

	if minutes == 1 {
		switch seconds {
		case 0:
			return "1 minute"
		case 1:
			return "1 minute 1 second"
		default:
			return fmt.Sprintf("1 minute %d seconds", seconds)
		}
	}

	switch seconds {
	case 0:
		return fmt.Sprintf("%d minutes", minutes)
	case 1:
		return fmt.Sprintf("%d minutes 1 second", minutes)
	default:
		return fmt.Sprintf("%d minutes %d seconds", minutes, seconds)
	}
}

// FormatDateTime formats a time.Time object into a string in the format "YYYY-MM-DD HH:MM:SS"
func FormatDateTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// FormatDateTimeFromUnix formats a Unix timestamp into a string in the format "YYYY-MM-DD HH:MM:SS"
func FormatDateTimeFromUnix(ts int64) string {
	t := time.Unix(ts, 0)
	return FormatDateTime(t)
}
