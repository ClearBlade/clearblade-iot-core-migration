package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	cb "github.com/clearblade/Go-SDK"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
	gcpiotpb "google.golang.org/genproto/googleapis/cloud/iot/v1"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

type Data struct {
	Project_id string `json:"project_id"`
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func readCsvFile(filePath string) [][]string {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatalln("Unable to read input file: ", filePath, err)
	}
	defer f.Close()

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		log.Fatalln("Unable to parse file as CSV for: ", filePath, err)
	}

	return records
}

func getProjectID(filePath string) string {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalln("Error when opening json file: ", err)
	}

	var payload Data
	err = json.Unmarshal(content, &payload)
	if err != nil {
		log.Fatalln("Error during Unmarshal(): ", err)
	}

	return payload.Project_id
}

func readInput(msg string) (string, error) {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)

	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	// remove the delimeter from the string
	return strings.TrimSuffix(input, "\n"), nil
}

func getProgressBar(total int, description, onCompletionText string) *progressbar.ProgressBar {
	description = string(colorYellow) + description + string(colorReset)
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println(string(colorGreen), "\n\u2713 ", onCompletionText, string(colorReset))
		}),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}))

	return bar
}

func getSpinner(description string) *progressbar.ProgressBar {
	description = string(colorYellow) + description + string(colorReset)
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
	)
	return bar
}

func getURI(region string) string {

	if Args.sandbox {
		return "https://iot-sandbox.clearblade.com"
	}

	return "https://community.clearblade.com"
	// return "https://" + region + ".clearblade.com"
}

func getAbsPath(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}

	if path[0] != '~' {
		return path, nil
	}

	if len(path) > 1 && path[1] != '/' && path[1] != '\\' {
		return "", errors.New("cannot expand user-specific home dir")
	}

	usr, _ := user.Current()
	dir := usr.HomeDir

	return filepath.Join(dir, path[1:]), nil
}

func mkDeviceKey(systemKey, name string) string {
	return systemKey + " :: " + name
}

func getFormatNumber(format gcpiotpb.PublicKeyFormat) cb.KeyFormat {
	switch format {
	case gcpiotpb.PublicKeyFormat_RSA_PEM:
		return 0
	case gcpiotpb.PublicKeyFormat_UNSPECIFIED_PUBLIC_KEY_FORMAT:
		return 0
	case gcpiotpb.PublicKeyFormat_ES256_PEM:
		return 1
	case gcpiotpb.PublicKeyFormat_RSA_X509_PEM:
		return 2
	case gcpiotpb.PublicKeyFormat_ES256_X509_PEM:
		return 3
	}

	return 0
}
