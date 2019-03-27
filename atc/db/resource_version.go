package db

import (
	"database/sql"
	"encoding/json"

	sq "github.com/Masterminds/squirrel"
	"github.com/concourse/concourse/atc"
)

//go:generate counterfeiter . ResourceVersion

type ResourceVersion interface {
	ID() int
	Space() atc.Space
	Version() Version
	Metadata() ResourceConfigMetadataFields
	Partial() bool
	CheckOrder() int
	ResourceConfig() ResourceConfig

	Reload() (bool, error)
}

type ResourceVersions []ResourceVersion

type ResourceConfigMetadataField struct {
	Name  string
	Value string
}

type ResourceConfigMetadataFields []ResourceConfigMetadataField

func NewResourceConfigMetadataFields(atcm []atc.MetadataField) ResourceConfigMetadataFields {
	metadata := make([]ResourceConfigMetadataField, len(atcm))
	for i, md := range atcm {
		metadata[i] = ResourceConfigMetadataField{
			Name:  md.Name,
			Value: md.Value,
		}
	}

	return metadata
}

func (rmf ResourceConfigMetadataFields) ToATCMetadata() []atc.MetadataField {
	metadata := make([]atc.MetadataField, len(rmf))
	for i, md := range rmf {
		metadata[i] = atc.MetadataField{
			Name:  md.Name,
			Value: md.Value,
		}
	}

	return metadata
}

type Version map[string]string

type resourceVersion struct {
	id         int
	space      atc.Space
	version    Version
	metadata   ResourceConfigMetadataFields
	partial    bool
	checkOrder int

	resourceConfig ResourceConfig

	conn Conn
}

var resourceVersionQuery = psql.Select(`
	v.id,
	v.version,
	v.metadata,
	v.partial,
	v.check_order,
	s.name
`).
	From("resource_versions v").
	LeftJoin("spaces s ON v.space_id = s.id").
	Where(sq.NotEq{
		"v.check_order": 0,
		"v.partial":     true,
	})

// This query is for finding ALL resource config versions (even ones that have a check order of 0)
// Do not use this query unless you are meaning to grab versions that are not yet validated
var uncheckedResourceVersionQuery = psql.Select(`
	v.id,
	v.version,
	v.metadata,
	v.partial,
	v.check_order,
	s.name
`).
	From("resource_versions v").
	LeftJoin("spaces s ON v.space_id = s.id")

func (r *resourceVersion) ID() int                                { return r.id }
func (r *resourceVersion) Space() atc.Space                       { return r.space }
func (r *resourceVersion) Version() Version                       { return r.version }
func (r *resourceVersion) Metadata() ResourceConfigMetadataFields { return r.metadata }
func (r *resourceVersion) Partial() bool                          { return r.partial }
func (r *resourceVersion) CheckOrder() int                        { return r.checkOrder }
func (r *resourceVersion) ResourceConfig() ResourceConfig {
	return r.resourceConfig
}

func (r *resourceVersion) Reload() (bool, error) {
	row := resourceVersionQuery.Where(sq.Eq{"v.id": r.id}).
		RunWith(r.conn).
		QueryRow()

	err := scanResourceVersion(r, row)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func scanResourceVersion(r *resourceVersion, scan scannable) error {
	var version, metadata sql.NullString

	err := scan.Scan(&r.id, &version, &metadata, &r.partial, &r.checkOrder, &r.space)
	if err != nil {
		return err
	}

	if version.Valid {
		err = json.Unmarshal([]byte(version.String), &r.version)
		if err != nil {
			return err
		}
	}

	if metadata.Valid {
		err = json.Unmarshal([]byte(metadata.String), &r.metadata)
		if err != nil {
			return err
		}
	}

	return nil
}
