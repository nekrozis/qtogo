package repository

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/nekrozis/qtogo/internal/model"
)

// Metadata is the document-level part of an Updates.xml: what describes the file
// itself rather than any one package.
//
// MetadataName names the `_meta.7z` companion and SHA1 digests it; both are
// absent from some older repositories (5.14 and 5.15 among them), so neither is
// required.
type Metadata struct {
	ApplicationName    string
	ApplicationVersion string
	Checksum           bool
	MetadataName       string
	SHA1               string
}

// PackageUpdate is one component the metadata offers.
//
// The element vocabulary is larger than installation needs: display-only fields
// such as DisplayName and Description, packaging hints such as Script and
// UserInterfaces, and rare elements (ForcedInstallation, Replaces, Licenses,
// Translations) all appear in the corpus. They are kept because this type is the
// lossless translation of the document, and discarding a field here would mean
// re-fetching the metadata to get it back.
type PackageUpdate struct {
	Name               string
	DisplayName        string
	Description        string
	Version            model.Version
	ReleaseDate        string
	Default            bool
	Virtual            bool
	ForcedInstallation bool
	SortingPriority    int
	Dependencies       []string
	AutoDependOn       []string
	Replaces           []string
	Script             string
	Downloadables      []string
	UserInterfaces     []string
	Translations       []string
	Licenses           []License
	UpdateFile         UpdateFile
	SHA1               string
	Operations         []Operation
}

// UpdateFile describes the component's update file and its sizes in bytes.
type UpdateFile struct {
	CompressedSize   int64
	UncompressedSize int64
	OS               string
}

// Operation is one entry of an <Operations> block, kept as written.
//
// Arguments are not interpreted: an Extract argument holds an install-time
// template such as "@TargetDir@/6.8.0/msvc2022_64", and expanding @TargetDir@
// needs the directory the user chose, which the parse layer does not know.
type Operation struct {
	Name      string
	Arguments []string
}

// License is one file a component asks to be shown or accepted.
type License struct {
	File string
	Name string
}

// ParseUpdatesXML reads a repository Updates.xml into the components it lists.
//
// The document is the authority on what a repository offers, so the parser is
// strict about the root element and about a component's version, and lenient
// about everything optional: an element the corpus does not always carry reads
// as its zero value rather than failing the whole document.
//
// sourceURL identifies the document in error messages; it is not fetched.
func ParseUpdatesXML(sourceURL string, body []byte) ([]PackageUpdate, Metadata, error) {
	label := sourceURL
	if label == "" {
		label = "Updates.xml"
	}

	var doc updatesDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, Metadata{}, fmt.Errorf("%s: %w", label, err)
	}

	meta := Metadata{
		ApplicationName:    doc.ApplicationName,
		ApplicationVersion: doc.ApplicationVersion,
		Checksum:           xmlBool(doc.Checksum),
		MetadataName:       doc.MetadataName,
		SHA1:               doc.SHA1,
	}

	packages := make([]PackageUpdate, 0, len(doc.Packages))
	for i, p := range doc.Packages {
		pkg, err := p.toPackage()
		if err != nil {
			return nil, Metadata{}, fmt.Errorf("%s: package %d (%s): %w", label, i, p.Name, err)
		}
		packages = append(packages, pkg)
	}
	return packages, meta, nil
}

func (p packageElement) toPackage() (PackageUpdate, error) {
	pkg := PackageUpdate{
		Name:               p.Name,
		DisplayName:        p.DisplayName,
		Description:        p.Description,
		ReleaseDate:        p.ReleaseDate,
		Default:            xmlBool(p.Default),
		Virtual:            xmlBool(p.Virtual),
		ForcedInstallation: xmlBool(p.ForcedInstallation),
		SortingPriority:    xmlInt(p.SortingPriority),
		Dependencies:       csvList(p.Dependencies),
		AutoDependOn:       csvList(p.AutoDependOn),
		Replaces:           csvList(p.Replaces),
		Script:             p.Script,
		Downloadables:      csvList(p.Downloadables),
		UserInterfaces:     csvList(p.UserInterfaces),
		Translations:       csvList(p.Translations),
		SHA1:               p.SHA1,
		UpdateFile: UpdateFile{
			CompressedSize:   p.UpdateFile.CompressedSize,
			UncompressedSize: p.UpdateFile.UncompressedSize,
			OS:               p.UpdateFile.OS,
		},
	}

	// A version element is always present in the corpus; an empty one is left
	// zero rather than treated as a malformed document.
	if strings.TrimSpace(p.Version) != "" {
		v, err := model.ParseVersion(p.Version)
		if err != nil {
			return PackageUpdate{}, err
		}
		pkg.Version = v
	}

	for _, l := range p.Licenses {
		pkg.Licenses = append(pkg.Licenses, License{File: l.File, Name: l.Name})
	}
	for _, o := range p.Operations {
		pkg.Operations = append(pkg.Operations, Operation{Name: o.Name, Arguments: o.Arguments})
	}
	return pkg, nil
}

// updatesDoc mirrors the XML with the shapes encoding/xml reads directly. The
// exported types above are built from it so the public shape stays clean.
type updatesDoc struct {
	XMLName            xml.Name         `xml:"Updates"`
	ApplicationName    string           `xml:"ApplicationName"`
	ApplicationVersion string           `xml:"ApplicationVersion"`
	Checksum           string           `xml:"Checksum"`
	Packages           []packageElement `xml:"PackageUpdate"`
	MetadataName       string           `xml:"MetadataName"`
	SHA1               string           `xml:"SHA1"`
}

type packageElement struct {
	Name               string           `xml:"Name"`
	DisplayName        string           `xml:"DisplayName"`
	Description        string           `xml:"Description"`
	Version            string           `xml:"Version"`
	ReleaseDate        string           `xml:"ReleaseDate"`
	Default            string           `xml:"Default"`
	Virtual            string           `xml:"Virtual"`
	ForcedInstallation string           `xml:"ForcedInstallation"`
	SortingPriority    string           `xml:"SortingPriority"`
	Dependencies       string           `xml:"Dependencies"`
	AutoDependOn       string           `xml:"AutoDependOn"`
	Replaces           string           `xml:"Replaces"`
	Script             string           `xml:"Script"`
	Downloadables      string           `xml:"DownloadableArchives"`
	UserInterfaces     string           `xml:"UserInterfaces"`
	Translations       string           `xml:"Translations"`
	Licenses           []licenseElement `xml:"Licenses>License"`
	UpdateFile         updateFileElement
	SHA1               string             `xml:"SHA1"`
	Operations         []operationElement `xml:"Operations>Operation"`
}

type updateFileElement struct {
	CompressedSize   int64  `xml:"CompressedSize,attr"`
	UncompressedSize int64  `xml:"UncompressedSize,attr"`
	OS               string `xml:"OS,attr"`
}

type operationElement struct {
	Name      string   `xml:"name,attr"`
	Arguments []string `xml:"Argument"`
}

type licenseElement struct {
	File string `xml:"file,attr"`
	Name string `xml:"name,attr"`
}

// csvList splits a comma-separated element value, trimming space and dropping
// empty items, so an absent element and an empty one read the same.
func csvList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func xmlBool(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }

// xmlInt reads a numeric element, treating an empty or unparsable value as zero:
// the field only orders components, so a bad value must not fail the document.
func xmlInt(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
