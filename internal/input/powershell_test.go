package input

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

// The starts of what native commands got from PowerShell 7.6 and Windows
// PowerShell 5.1, piped from Invoke-WebRequest and Invoke-RestMethod.
var (
	pwshWebRequest = "\r\nStatusCode        : 200\r\nStatusDescription : OK\r\n" +
		`Content           : {"id":1449118280,"name":"jitto","owner":{"login":"ibuki-aizawa","avatar_url":"h…` + "\r\n" +
		"RawContent        : HTTP/1.1 200 OK\r\n" +
		"                    Date: Sun, 08 Nov 2026 07:11:53 GMT\r\n" +
		"                    Vary: Accept\r\n" +
		"Headers           : {[Date, System.String[]], [Cache-Contro"
	winPSWebRequest = bom + "\r\n\r\nStatusCode        : 200\r\nStatusDescription : OK\r\n" +
		`Content           : {"id":1449118280,"node_id":"R_kgDOVl_KSA","name":"jitto","full_name":"ibuki-aizawa/jitto","private"` + "\r\n" +
		`                    :false,"owner":{"login":"ibuki-aizawa","id":83331063,"node_id":"MDQ6VXNlcjgzMzMxMDYz","avatar_url":` + "\r\n" +
		`                    "h...` + "\r\n" +
		"Forms             : \r\n" +
		"Headers           : {[Vary, Accept,Accept-Encodi"
	pwshRestMethod = "\r\nid                           : 1449118280\r\n" +
		"node_id                      : R_kgDOVl_KSA\r\n" +
		"name                         : jitto\r\n" +
		"private                      : False\r\n" +
		"owner                        : @{login=ibuki-aizawa; id=83331063}\r\n" +
		"\r\nid                           : 2\r\n"
)

func TestLooksLikePowerShellDisplay(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"pwsh Invoke-WebRequest", pwshWebRequest, true},
		{"Windows PowerShell Invoke-WebRequest", strings.TrimPrefix(winPSWebRequest, bom), true},
		{"pwsh Invoke-RestMethod", pwshRestMethod, true},
		{"wide name, pwsh", "\r\n名前 : 太郎\r\na-b  : 1\r\nx    : 2\r\n\r\n", true},
		{"wide name, Windows PowerShell", "\r\n\r\n名前  : 太郎\r\na-b : 1\r\nx   : 2\r\n\r\n", true},
		{"wide name, neither", "\r\n名前   : 太郎\r\na-b : 1\r\n", false},
		{"one property", "\r\nStatusCode : 200\r\n", false},
		{"cut off after one property", "\r\nStatusCode        : 200\r\nStatusDesc", false},
		{"LF line ends", strings.ReplaceAll(pwshWebRequest, "\r\n", "\n"), false},
		{"colons not lined up", "a : 1\r\nbb : 2\r\n", false},
		{"no space before the colon", "a: 1\r\nb: 2\r\n", false},
		{"no space after the colon", "a :1\r\nb :2\r\n", false},
		{"line that is not a property", "a : 1\r\nnot a property\r\nb : 2\r\n", false},
		{"continuation not indented enough", "a : 1\r\n  x\r\nb : 2\r\n", false},
		{"table", "\r\nName  Value\r\n----  -----\r\na     1\r\n", false},
		{"JSON", "{\r\n  \"a\": 1\r\n}\r\n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLikePowerShellDisplay([]byte(tt.text)); got != tt.want {
				t.Errorf("LooksLikePowerShellDisplay(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestReadPowerShellDisplay(t *testing.T) {
	for name, text := range map[string]string{
		"pwsh Invoke-WebRequest":               pwshWebRequest,
		"Windows PowerShell Invoke-WebRequest": winPSWebRequest,
		"pwsh Invoke-RestMethod":               pwshRestMethod,
	} {
		t.Run(name, func(t *testing.T) {
			// A pipe can deliver the text a little at a time.
			_, _, err := Read(iotest.OneByteReader(strings.NewReader(text)), UTF8)
			if !errors.Is(err, ErrPowerShellDisplay) {
				t.Fatalf("err = %v, want ErrPowerShellDisplay", err)
			}
			if strings.Contains(err.Error(), ErrPowerShellDisplay.Error()) {
				t.Errorf("err = %q, want the message unchanged", err)
			}
			if !strings.HasPrefix(err.Error(), "invalid JSON in document 1") {
				t.Errorf("err = %q, want an invalid JSON error", err)
			}
		})
	}
}

func TestReadPowerShellDisplayUTF16(t *testing.T) {
	// Windows PowerShell 5.1 writes UTF-16 files with ">".
	_, _, err := Read(strings.NewReader(utf16Bytes(pwshRestMethod, binary.LittleEndian)), UTF8)
	if !errors.Is(err, ErrPowerShellDisplay) {
		t.Fatalf("err = %v, want ErrPowerShellDisplay", err)
	}
}

func TestReadNotPowerShellDisplay(t *testing.T) {
	for name, text := range map[string]string{
		"invalid JSON":          "{\"a\": 1,}",
		"second document":       "{\"a\": 1}\r\n" + pwshRestMethod,
		"letters":               "abc",
		"key and value":         "a : 1\r\n",
		"unexpected end":        "{\"a\": ",
		"display after a value": "1 " + pwshWebRequest,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Read(strings.NewReader(text), UTF8)
			if err == nil {
				t.Fatal("Read succeeded, want an error")
			}
			if errors.Is(err, ErrPowerShellDisplay) {
				t.Errorf("err = %v, want it not to be ErrPowerShellDisplay", err)
			}
		})
	}
}

func TestReadPowerShellDisplayKeepsInvalidUTF8(t *testing.T) {
	text := "\r\nname : \xff\r\nid   : 1\r\n"
	_, _, err := Read(iotest.OneByteReader(strings.NewReader(text)), UTF8)
	if !errors.Is(err, ErrPowerShellDisplay) {
		t.Errorf("err = %v, want ErrPowerShellDisplay", err)
	}
	// The invalid byte comes after the error, at the first letter.
	if errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("err = %v, want it not to be ErrInvalidUTF8", err)
	}
}
