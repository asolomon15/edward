package edward_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"github.com/theothertomelliott/must"
	"github.com/yext/edward/common"
	"github.com/yext/edward/config"
	"github.com/yext/edward/edward"
	"github.com/yext/edward/home"
)

func TestGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	var tests = []struct {
		name             string
		path             string
		config           string
		services         []string
		group            string
		targets          []string
		force            bool
		input            string
		expectedOutput   string
		expectedServices []string
		expectedGroups   map[string][]string
		err              error
	}{
		{
			name:             "existing config and services",
			path:             "testdata/generate/singlewithconfig",
			config:           "edward.json",
			expectedOutput:   "No new services, groups or imports found\n",
			expectedServices: []string{"edward-test-service"},
		},
		{
			name:             "existing config and services - forced",
			path:             "testdata/generate/singlewithconfig",
			config:           "edward.json",
			expectedOutput:   "No new services, groups or imports found\n",
			force:            true,
			expectedServices: []string{"edward-test-service"},
		},
		{
			name:   "existing empty config file",
			path:   "testdata/generate/emptyconfig",
			config: "edward.json",
			input:  "Y\n",
			expectedOutput: `The following will be generated:
Services:
	edward-test-service
Do you wish to continue? [y/n]? Wrote to: ${TMP_PATH}/edward.json
`,
			expectedServices: []string{"edward-test-service"},
		},
		{
			name:   "duplicates",
			path:   "testdata/generate/duplicatenames",
			config: "edward.json",
			force:  true,
			err:    errors.New("Multiple services or groups were found with the names: edward-test-service"),
		},
		{
			name:   "new config and service",
			path:   "testdata/generate/single",
			config: "edward.json",
			input:  "Y\n",
			expectedOutput: `The following will be generated:
Services:
	edward-test-service
Do you wish to continue? [y/n]? Wrote to: ${TMP_PATH}/edward.json
`,
			expectedServices: []string{"edward-test-service"},
		},
		{
			name:   "new config and service - forced",
			path:   "testdata/generate/single",
			config: "edward.json",
			force:  true,
			expectedOutput: `Wrote to: ${TMP_PATH}/edward.json
`,
			expectedServices: []string{"edward-test-service"},
		},
		{
			name:   "new config and service with group",
			path:   "testdata/generate/single",
			config: "edward.json",
			group:  "newgroup",
			input:  "Y\n",
			expectedOutput: `The following will be generated:
Services:
	edward-test-service
Do you wish to continue? [y/n]? Wrote to: ${TMP_PATH}/edward.json
`,
			expectedServices: []string{"edward-test-service"},
			expectedGroups:   map[string][]string{"newgroup": []string{"edward-test-service"}},
		},
		{
			name:   "new config and service with existing group",
			path:   "testdata/generate/groupwithconfig",
			config: "edward.json",
			group:  "group1",
			input:  "Y\n",
			expectedOutput: `The following will be generated:
Services:
	edward-test-service2
Do you wish to continue? [y/n]? Wrote to: ${TMP_PATH}/edward.json
`,
			expectedServices: []string{"edward-test-service", "edward-test-service2"},
			expectedGroups:   map[string][]string{"group1": []string{"edward-test-service", "edward-test-service2"}},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			// Set up edward home directory
			if err := home.EdwardConfig.Initialize(); err != nil {
				t.Fatal(err)
			}

			var err error

			// Copy test content into a temp dir on the GOPATH & defer deletion
			cleanup := createWorkingDir(t, test.name, test.path)
			defer cleanup()

			client := edward.NewClient()
			client.EdwardExecutable = edwardExecutable
			client.DisableConcurrentPhases = true

			// Set up input and output for the client
			var outputReader, inputReader *io.PipeReader
			var inputWriter, outputWriter *io.PipeWriter
			inputReader, inputWriter = io.Pipe()
			outputReader, outputWriter = io.Pipe()

			client.Output = outputWriter
			client.Input = inputReader

			var ioWg sync.WaitGroup
			ioWg.Add(2)
			go func() {
				if len(test.input) > 0 {
					fmt.Fprint(inputWriter, test.input)
				}
				ioWg.Done()
			}()

			var output string
			go func() {
				outBytes, err := ioutil.ReadAll(outputReader)
				if err != nil {
					t.Fatal(err)
				}
				output = string(outBytes)
				ioWg.Done()
			}()

			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}

			err = client.Generate(test.services, test.force, test.group, test.targets)
			inputWriter.Close()
			outputWriter.Close()
			must.BeEqualErrors(t, test.err, err)
			if err != nil {
				return
			}

			ioWg.Wait()

			expectedOutput := strings.Replace(test.expectedOutput, "${TMP_PATH}", cwd, 1)
			must.BeEqual(t, expectedOutput, output)

			cfg, err := config.LoadConfig(test.config, common.EdwardVersion, client.Logger)
			if err != nil {
				t.Error(err)
				return
			}

			var services []string
			var groups []string
			for _, service := range cfg.ServiceMap {
				services = append(services, service.Name)
			}
			sort.Strings(services)
			for _, group := range cfg.GroupMap {
				groups = append(groups, group.Name)
			}
			sort.Strings(groups)

			must.BeEqual(t, test.expectedServices, services)
			for groupName, expectedChildren := range test.expectedGroups {
				if group, ok := cfg.GroupMap[groupName]; ok {
					var children []string
					for _, childService := range group.Services {
						children = append(children, childService.Name)
					}
					for _, childGroup := range group.Groups {
						children = append(children, childGroup.Name)
					}
					sort.Strings(children)
					must.BeEqual(t, expectedChildren, children, fmt.Sprintf("Children for group '%s' did not match\n", group.Name))
				} else {
					t.Errorf("Group not found %s", groupName)
				}
			}
		})
	}
}
