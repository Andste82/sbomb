package msbuild

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

type Project struct {
	Sources        []string
	Configuration  string
	Platform       string
	Configurations []string
}

func ParseProject(path, configuration, platform string) (Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Project{}, err
	}
	var file vcxProject
	if err := xml.Unmarshal(data, &file); err != nil {
		return Project{}, err
	}
	project := Project{Configuration: configuration, Platform: platform}
	targetConfig := ""
	if configuration != "" && platform != "" {
		targetConfig = configuration + "|" + platform
	}
	for _, group := range file.ItemGroups {
		if group.Condition != "" && targetConfig != "" && !strings.Contains(group.Condition, targetConfig) {
			continue
		}
		for _, cfgItem := range group.ProjectConfiguration {
			if cfgItem.Include != "" {
				project.Configurations = append(project.Configurations, cfgItem.Include)
			}
		}
		for _, source := range group.ClCompile {
			if source.Include != "" {
				if source.Condition == "" || targetConfig == "" || strings.Contains(source.Condition, targetConfig) {
					project.Sources = append(project.Sources, source.Include)
				}
			}
		}
		for _, resource := range group.ResourceCompile {
			if resource.Include != "" {
				if resource.Condition == "" || targetConfig == "" || strings.Contains(resource.Condition, targetConfig) {
					project.Sources = append(project.Sources, resource.Include)
				}
			}
		}
	}
	return project, nil
}

type vcxProject struct {
	ItemGroups []itemGroup `xml:"ItemGroup"`
}
type itemGroup struct {
	Condition            string        `xml:"Condition,attr"`
	ProjectConfiguration []includeItem `xml:"ProjectConfiguration"`
	ClCompile            []includeItem `xml:"ClCompile"`
	ResourceCompile      []includeItem `xml:"ResourceCompile"`
}
type includeItem struct {
	Include   string `xml:"Include,attr"`
	Condition string `xml:"Condition,attr"`
}

func ParseShowIncludes(text string) (string, []string, error) {
	var prefix string
	var headers []string
	for _, line := range strings.Split(text, "\n") {
		if prefix == "" {
			lower := strings.ToLower(line)
			index := strings.Index(line, ": C:")
			if index < 0 {
				index = strings.Index(line, ": /")
			}
			if index >= 0 && (strings.Contains(lower[:index], "including") || strings.Contains(lower[:index], "eingeschlossen")) {
				prefix = line[:index+1]
			}
		}
		if prefix != "" && strings.HasPrefix(line, prefix) {
			path := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if path != "" {
				headers = append(headers, path)
			}
		}
	}
	if prefix == "" {
		return "", nil, fmt.Errorf("/showIncludes prefix not found")
	}
	return prefix, headers, nil
}
