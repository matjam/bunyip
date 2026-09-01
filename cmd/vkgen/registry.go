package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// The XML model mirrors the parts of vk.xml the generator reads. Attributes
// the generator does not use are not declared.

type registry struct {
	Platforms  []platform  `xml:"platforms>platform"`
	Types      []typeDef   `xml:"types>type"`
	Enums      []enumBlock `xml:"enums"`
	Commands   []command   `xml:"commands>command"`
	Features   []feature   `xml:"feature"`
	Extensions []extension `xml:"extensions>extension"`
}

type platform struct {
	Name    string `xml:"name,attr"`
	Protect string `xml:"protect,attr"`
}

type typeDef struct {
	Category  string   `xml:"category,attr"`
	NameAttr  string   `xml:"name,attr"`
	NameElem  string   `xml:"name"`
	ProtoName string   `xml:"proto>name"`
	Alias     string   `xml:"alias,attr"`
	Requires  string   `xml:"requires,attr"`
	BitValues string   `xml:"bitvalues,attr"`
	API       string   `xml:"api,attr"`
	Members   []member `xml:"member"`
	Inner     string   `xml:",innerxml"`
}

func (t typeDef) name() string {
	if t.NameAttr != "" {
		return t.NameAttr
	}
	if t.NameElem != "" {
		return t.NameElem
	}
	return t.ProtoName
}

type member struct {
	API   string `xml:"api,attr"`
	Inner string `xml:",innerxml"`
}

type enumBlock struct {
	Name     string      `xml:"name,attr"`
	Type     string      `xml:"type,attr"`
	BitWidth string      `xml:"bitwidth,attr"`
	Entries  []enumEntry `xml:"enum"`
}

type enumEntry struct {
	Name      string `xml:"name,attr"`
	Value     string `xml:"value,attr"`
	BitPos    string `xml:"bitpos,attr"`
	Offset    string `xml:"offset,attr"`
	Extends   string `xml:"extends,attr"`
	ExtNumber string `xml:"extnumber,attr"`
	Dir       string `xml:"dir,attr"`
	Alias     string `xml:"alias,attr"`
	Type      string `xml:"type,attr"`
	API       string `xml:"api,attr"`
	Protect   string `xml:"protect,attr"`
}

type command struct {
	NameAttr string  `xml:"name,attr"`
	Alias    string  `xml:"alias,attr"`
	API      string  `xml:"api,attr"`
	Proto    proto   `xml:"proto"`
	Params   []param `xml:"param"`
}

func (c command) name() string {
	if c.NameAttr != "" {
		return c.NameAttr
	}
	return c.Proto.Name
}

type proto struct {
	Type string `xml:"type"`
	Name string `xml:"name"`
}

type param struct {
	API   string `xml:"api,attr"`
	Inner string `xml:",innerxml"`
}

type feature struct {
	API      string    `xml:"api,attr"`
	Name     string    `xml:"name,attr"`
	Number   string    `xml:"number,attr"`
	Requires []require `xml:"require"`
}

type extension struct {
	Name      string    `xml:"name,attr"`
	Number    string    `xml:"number,attr"`
	Supported string    `xml:"supported,attr"`
	Platform  string    `xml:"platform,attr"`
	Requires  []require `xml:"require"`
}

type require struct {
	API      string      `xml:"api,attr"`
	Types    []nameRef   `xml:"type"`
	Enums    []enumEntry `xml:"enum"`
	Commands []nameRef   `xml:"command"`
}

type nameRef struct {
	Name string `xml:"name,attr"`
}

func loadRegistry(path string) (*registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var reg registry
	if err := xml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", path, err)
	}
	return &reg, nil
}

// forVulkan reports whether an api attribute admits the Vulkan (not Vulkan SC)
// API. An empty attribute applies to every API.
func forVulkan(api string) bool {
	if api == "" {
		return true
	}
	for _, a := range strings.Split(api, ",") {
		if a == "vulkan" {
			return true
		}
	}
	return false
}
