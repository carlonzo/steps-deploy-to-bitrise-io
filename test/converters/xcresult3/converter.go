package xcresult3

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/log"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/go-xcode/xcodeproject/serialized"
	"github.com/bitrise-steplib/steps-deploy-to-bitrise-io/test/junit"
	"howett.net/plist"
)

// Converter ...
type Converter struct {
	xcresultPth string
}

func majorVersion(document serialized.Object) (int, error) {
	version, err := document.Object("version")
	if err != nil {
		return -1, err
	}

	major, err := version.Value("major")
	if err != nil {
		return -1, err
	}
	return int(major.(uint64)), nil
}

func documentMajorVersion(pth string) (int, error) {
	content, err := fileutil.ReadBytesFromFile(pth)
	if err != nil {
		return -1, err
	}

	var info serialized.Object
	if _, err := plist.Unmarshal(content, &info); err != nil {
		return -1, err
	}

	return majorVersion(info)
}

// Detect ...
func (c *Converter) Detect(files []string) bool {
	if !isXcresulttoolAvailable() {
		log.Debugf("xcresult tool is not available")
		return false
	}

	for _, file := range files {
		if filepath.Ext(file) != ".xcresult" {
			continue
		}

		infoPth := filepath.Join(file, "Info.plist")
		if exist, err := pathutil.IsPathExists(infoPth); err != nil {
			log.Debugf("Failed to find Info.plist at %s: %s", infoPth, err)
			continue
		} else if !exist {
			log.Debugf("No Info.plist found at %s", infoPth)
			continue
		}

		version, err := documentMajorVersion(infoPth)
		if err != nil {
			log.Debugf("failed to get document version: %s", err)
			continue
		}

		if version < 3 {
			log.Debugf("version < 3: %d", version)
			continue
		}

		c.xcresultPth = file
		return true
	}
	return false
}

// XML ...
func (c *Converter) XML() (junit.XML, error) {
	testResultDir := filepath.Dir(c.xcresultPth)

	_, summaries, err := Parse(c.xcresultPth)
	if err != nil {
		return junit.XML{}, err
	}

	var xmlData junit.XML
	for _, summary := range summaries {
		testSuiteOrder, testsByName := summary.tests()

		for _, name := range testSuiteOrder {
			tests := testsByName[name]

			testSuit := junit.TestSuite{
				Name:     name,
				Tests:    len(tests),
				Failures: summary.failuresCount(name),
				Skipped:  summary.skippedCount(name),
				Time:     summary.totalTime(name),
			}

			for _, test := range tests {
				var duartion float64
				if test.Duration.Value != "" {
					duartion, err = strconv.ParseFloat(test.Duration.Value, 64)
					if err != nil {
						return junit.XML{}, err
					}
				}

				var failure *junit.Failure
				var skipped *junit.Skipped
				switch test.TestStatus.Value {
				case "Failure":
					testSummary, err := test.loadActionTestSummary(c.xcresultPth)
					if err != nil {
						return junit.XML{}, err
					}

					failureMessage := ""
					for _, aTestFailureSummary := range testSummary.FailureSummaries.Values {
						file := aTestFailureSummary.FileName.Value
						line := aTestFailureSummary.LineNumber.Value
						message := aTestFailureSummary.Message.Value

						if len(failureMessage) > 0 {
							failureMessage += "\n"
						}
						failureMessage += fmt.Sprintf("%s:%s - %s", file, line, message)
					}

					failure = &junit.Failure{
						Value: failureMessage,
					}
				case "Skipped":
					skipped = &junit.Skipped{}
				}

				testSuit.TestCases = append(testSuit.TestCases, junit.TestCase{
					Name:      test.Name.Value,
					ClassName: strings.Split(test.Identifier.Value, "/")[0],
					Failure:   failure,
					Skipped:   skipped,
					Time:      duartion,
				})

				if err := test.exportScreenshots(c.xcresultPth, testResultDir); err != nil {
					return junit.XML{}, err
				}
			}

			xmlData.TestSuites = append(xmlData.TestSuites, testSuit)
		}
	}

	return xmlData, nil
}
