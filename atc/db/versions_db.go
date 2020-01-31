package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/concourse/concourse/atc"
	gocache "github.com/patrickmn/go-cache"
)

type VersionsDB struct {
	conn      Conn
	limitRows int

	cache *gocache.Cache
}

func NewVersionsDB(conn Conn, limitRows int, cache *gocache.Cache) VersionsDB {
	return VersionsDB{
		conn:      conn,
		limitRows: limitRows,
		cache:     cache,
	}
}

func (versions VersionsDB) VersionIsDisabled(ctx context.Context, resourceID int, versionMD5 ResourceVersion) (bool, error) {
	var exists bool
	err := versions.conn.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM resource_disabled_versions
			WHERE resource_id = $1
			AND version_md5 = $2
		)`, resourceID, versionMD5).
		Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func (versions VersionsDB) LatestVersionOfResource(ctx context.Context, resourceID int) (ResourceVersion, bool, error) {
	tx, err := versions.conn.Begin()
	if err != nil {
		return "", false, err
	}

	defer tx.Rollback()

	version, found, err := versions.latestVersionOfResource(ctx, tx, resourceID)
	if err != nil {
		return "", false, err
	}

	if !found {
		return "", false, nil
	}

	err = tx.Commit()
	if err != nil {
		return "", false, err
	}

	return version, true, nil
}

func (versions VersionsDB) SuccessfulBuilds(ctx context.Context, jobID int) PaginatedBuilds {
	builder := psql.Select("id", "rerun_of").
		From("builds").
		Where(sq.Eq{
			"job_id": jobID,
			"status": "succeeded",
		}).
		OrderBy("COALESCE(rerun_of, id) DESC, id DESC")

	return PaginatedBuilds{
		builder: builder,
		column:  "id",
		jobID:   jobID,

		limitRows: versions.limitRows,
		conn:      versions.conn,
	}
}

func (versions VersionsDB) SuccessfulBuildsVersionConstrained(
	ctx context.Context,
	jobID int,
	constrainingCandidates map[string][]string,
) (PaginatedBuilds, error) {
	versionsJSON, err := json.Marshal(constrainingCandidates)
	if err != nil {
		return PaginatedBuilds{}, err
	}

	builder := psql.Select("build_id", "rerun_of").
		From("successful_build_outputs").
		Where(sq.Expr("outputs @> ?::jsonb", versionsJSON)).
		Where(sq.Eq{
			"job_id": jobID,
		}).
		OrderBy("COALESCE(rerun_of, build_id) DESC, build_id DESC")

	return PaginatedBuilds{
		builder: builder,
		column:  "build_id",
		jobID:   jobID,

		limitRows: versions.limitRows,
		conn:      versions.conn,
	}, nil
}

func (versions VersionsDB) BuildOutputs(ctx context.Context, buildID int) ([]AlgorithmOutput, error) {
	uniqOutputs := map[string]AlgorithmOutput{}
	rows, err := psql.Select("name", "resource_id", "version_md5").
		From("build_resource_config_version_inputs").
		Where(sq.Eq{"build_id": buildID}).
		RunWith(versions.conn).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var output AlgorithmOutput
		err := rows.Scan(&output.InputName, &output.ResourceID, &output.Version)
		if err != nil {
			return nil, err
		}

		uniqOutputs[output.InputName] = output
	}

	rows, err = psql.Select("name", "resource_id", "version_md5").
		From("build_resource_config_version_outputs").
		Where(sq.Eq{"build_id": buildID}).
		RunWith(versions.conn).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var output AlgorithmOutput
		err := rows.Scan(&output.InputName, &output.ResourceID, &output.Version)
		if err != nil {
			return nil, err
		}

		uniqOutputs[output.InputName] = output
	}

	outputs := []AlgorithmOutput{}
	for _, o := range uniqOutputs {
		outputs = append(outputs, o)
	}

	sort.Slice(outputs, func(i, j int) bool {
		return outputs[i].InputName > outputs[j].InputName
	})

	return outputs, nil
}

type resourceOutputs struct {
	ResourceID int
	Versions   []string
}

func (versions VersionsDB) SuccessfulBuildOutputs(ctx context.Context, buildID int) ([]AlgorithmVersion, error) {
	cacheKey := fmt.Sprintf("o%d", buildID)

	c, found := versions.cache.Get(cacheKey)
	if found {
		return c.([]AlgorithmVersion), nil
	}

	var outputsJSON string
	err := psql.Select("outputs").
		From("successful_build_outputs").
		Where(sq.Eq{"build_id": buildID}).
		RunWith(versions.conn).
		QueryRowContext(ctx).
		Scan(&outputsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			outputsJSON, err = versions.migrateSingle(ctx, buildID)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	outputs := map[string][]string{}
	err = json.Unmarshal([]byte(outputsJSON), &outputs)
	if err != nil {
		return nil, err
	}

	byResourceID := []resourceOutputs{}
	for resourceIDStr, versions := range outputs {
		resourceID, err := strconv.Atoi(resourceIDStr)
		if err != nil {
			return nil, err
		}

		byResourceID = append(byResourceID, resourceOutputs{
			ResourceID: resourceID,
			Versions:   versions,
		})
	}

	sort.Slice(byResourceID, func(i, j int) bool {
		return byResourceID[i].ResourceID < byResourceID[j].ResourceID
	})

	algorithmOutputs := []AlgorithmVersion{}
	for _, outputs := range byResourceID {
		for _, version := range outputs.Versions {
			algorithmOutputs = append(algorithmOutputs, AlgorithmVersion{
				ResourceID: outputs.ResourceID,
				Version:    ResourceVersion(version),
			})
		}
	}

	versions.cache.Set(cacheKey, algorithmOutputs, time.Hour)

	return algorithmOutputs, nil
}

func (versions VersionsDB) FindVersionOfResource(ctx context.Context, resourceID int, v atc.Version) (ResourceVersion, bool, error) {
	versionJSON, err := json.Marshal(v)
	if err != nil {
		return "", false, nil
	}

	cacheKey := fmt.Sprintf("v%d-%s", resourceID, versionJSON)

	c, found := versions.cache.Get(cacheKey)
	if found {
		return c.(ResourceVersion), true, nil
	}

	var version ResourceVersion
	err = psql.Select("rcv.version_md5").
		From("resource_config_versions rcv").
		Join("resources r ON r.resource_config_scope_id = rcv.resource_config_scope_id").
		Where(sq.Eq{
			"r.id": resourceID,
		}).
		Where(sq.Expr("rcv.version_md5 = md5(?)", versionJSON)).
		RunWith(versions.conn).
		QueryRowContext(ctx).
		Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}

	versions.cache.Set(cacheKey, version, time.Hour)

	return version, true, err
}

func (versions VersionsDB) LatestBuildID(ctx context.Context, jobID int) (int, bool, error) {
	var buildID int
	err := psql.Select("b.id").
		From("builds b").
		Where(sq.Eq{
			"b.job_id":       jobID,
			"b.inputs_ready": true,
			"b.scheduled":    true,
		}).
		OrderBy("COALESCE(b.rerun_of, b.id) DESC, b.id DESC").
		Limit(100).
		RunWith(versions.conn).
		QueryRowContext(ctx).
		Scan(&buildID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, err
	}

	return buildID, true, nil
}

func (versions VersionsDB) NextEveryVersion(ctx context.Context, buildID int, resourceID int) (ResourceVersion, bool, bool, error) {
	tx, err := versions.conn.Begin()
	if err != nil {
		return "", false, false, err
	}

	defer tx.Rollback()

	var checkOrder int
	err = psql.Select("rcv.check_order").
		From("build_resource_config_version_inputs i").
		Join("resource_config_versions rcv ON rcv.resource_config_scope_id = (SELECT resource_config_scope_id FROM resources WHERE id = ?)", resourceID).
		Where(sq.Expr("i.version_md5 = rcv.version_md5")).
		Where(sq.Eq{"i.build_id": buildID}).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&checkOrder)
	if err != nil {
		if err == sql.ErrNoRows {
			version, found, err := versions.latestVersionOfResource(ctx, tx, resourceID)
			if err != nil {
				return "", false, false, err
			}

			if !found {
				return "", false, false, nil
			}

			err = tx.Commit()
			if err != nil {
				return "", false, false, err
			}

			return version, false, true, nil
		}

		return "", false, false, err
	}

	var nextVersion ResourceVersion
	rows, err := psql.Select("rcv.version_md5").
		From("resource_config_versions rcv").
		Where(sq.Expr("rcv.resource_config_scope_id = (SELECT resource_config_scope_id FROM resources WHERE id = ?)", resourceID)).
		Where(sq.Expr("NOT EXISTS (SELECT 1 FROM resource_disabled_versions WHERE resource_id = ? AND version_md5 = rcv.version_md5)", resourceID)).
		Where(sq.Gt{"rcv.check_order": checkOrder}).
		OrderBy("rcv.check_order ASC").
		Limit(2).
		RunWith(tx).
		QueryContext(ctx)
	if err != nil {
		return "", false, false, err
	}

	if rows.Next() {
		err = rows.Scan(&nextVersion)
		if err != nil {
			return "", false, false, err
		}

		var hasNext bool
		if rows.Next() {
			hasNext = true
		}

		rows.Close()

		err = tx.Commit()
		if err != nil {
			return "", false, false, err
		}

		return nextVersion, hasNext, true, nil
	}

	err = psql.Select("rcv.version_md5").
		From("resource_config_versions rcv").
		Where(sq.Expr("rcv.resource_config_scope_id = (SELECT resource_config_scope_id FROM resources WHERE id = ?)", resourceID)).
		Where(sq.Expr("NOT EXISTS (SELECT 1 FROM resource_disabled_versions WHERE resource_id = ? AND version_md5 = rcv.version_md5)", resourceID)).
		Where(sq.LtOrEq{"rcv.check_order": checkOrder}).
		OrderBy("rcv.check_order DESC").
		Limit(1).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&nextVersion)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, false, nil
		}
		return "", false, false, err
	}

	err = tx.Commit()
	if err != nil {
		return "", false, false, err
	}

	return nextVersion, false, true, nil
}

func (versions VersionsDB) LatestBuildPipes(ctx context.Context, buildID int) (map[int]int, error) {
	rows, err := psql.Select("p.from_build_id", "b.job_id").
		From("build_pipes p").
		Join("builds b ON b.id = p.from_build_id").
		Where(sq.Eq{
			"p.to_build_id": buildID,
		}).
		RunWith(versions.conn).
		QueryContext(ctx)
	if err != nil {
		return nil, err
	}

	jobToBuildPipes := map[int]int{}
	for rows.Next() {
		var buildID int
		var jobID int

		err = rows.Scan(&buildID, &jobID)
		if err != nil {
			return nil, err
		}

		jobToBuildPipes[jobID] = buildID
	}

	return jobToBuildPipes, nil
}

func (versions VersionsDB) UnusedBuilds(ctx context.Context, buildID int, jobID int) (PaginatedBuilds, error) {
	rows, err := psql.Select("id", "rerun_of").
		From("builds").
		Where(sq.And{
			sq.Eq{
				"job_id": jobID,
				"status": "succeeded",
			},
			sq.Or{
				sq.And{
					sq.Gt{
						"rerun_of": buildID,
					},
					sq.NotEq{
						"rerun_of": nil,
					},
				},
				sq.And{
					sq.Gt{
						"id": buildID,
					},
					sq.Eq{
						"rerun_of": nil,
					},
				},
			},
		}).
		OrderBy("COALESCE(rerun_of, id) ASC, id ASC").
		RunWith(versions.conn).
		QueryContext(ctx)
	if err != nil {
		return PaginatedBuilds{}, err
	}

	var builds []pagedBuild
	for rows.Next() {
		var build pagedBuild
		err = rows.Scan(&build.id, &build.rerunOf)
		if err != nil {
			return PaginatedBuilds{}, err
		}

		builds = append(builds, build)
	}

	builder := psql.Select("id", "rerun_of").
		From("builds").
		Where(sq.And{
			sq.LtOrEq{"id": buildID},
			sq.Eq{
				"job_id": jobID,
				"status": "succeeded",
			},
		}).
		OrderBy("COALESCE(rerun_of, id) DESC, id DESC")

	return PaginatedBuilds{
		builder:      builder,
		builds:       builds,
		unusedBuilds: true,

		column: "id",
		jobID:  jobID,

		limitRows: versions.limitRows,
		conn:      versions.conn,
	}, nil
}

func (versions VersionsDB) UnusedBuildsVersionConstrained(ctx context.Context, buildID int, jobID int, constrainingCandidates map[string][]string) (PaginatedBuilds, error) {
	versionsJSON, err := json.Marshal(constrainingCandidates)
	if err != nil {
		return PaginatedBuilds{}, err
	}

	rows, err := psql.Select("id", "rerun_of").
		From("builds").
		Where(sq.And{
			sq.Eq{
				"job_id": jobID,
				"status": "succeeded",
			},
			sq.Or{
				sq.And{
					sq.Gt{
						"rerun_of": buildID,
					},
					sq.NotEq{
						"rerun_of": nil,
					},
				},
				sq.And{
					sq.Gt{
						"id": buildID,
					},
					sq.Eq{
						"rerun_of": nil,
					},
				},
			},
		}).
		OrderBy("COALESCE(rerun_of, id) ASC, id ASC").
		RunWith(versions.conn).
		QueryContext(ctx)
	if err != nil {
		return PaginatedBuilds{}, err
	}

	var builds []pagedBuild
	for rows.Next() {
		var build pagedBuild
		err = rows.Scan(&build.id, &build.rerunOf)
		if err != nil {
			return PaginatedBuilds{}, err
		}

		builds = append(builds, build)
	}

	builder := psql.Select("build_id", "rerun_of").
		From("successful_build_outputs").
		Where(sq.Expr("outputs @> ?::jsonb", versionsJSON)).
		Where(sq.Eq{
			"job_id": jobID,
		}).
		Where(sq.LtOrEq{
			"build_id": buildID,
		}).
		OrderBy("COALESCE(rerun_of, build_id) DESC, build_id DESC")

	return PaginatedBuilds{
		builder:      builder,
		builds:       builds,
		unusedBuilds: true,

		column: "build_id",
		jobID:  jobID,

		limitRows: versions.limitRows,
		conn:      versions.conn,
	}, nil

}

func (versions VersionsDB) latestVersionOfResource(ctx context.Context, tx Tx, resourceID int) (ResourceVersion, bool, error) {
	var scopeID sql.NullInt64
	err := psql.Select("resource_config_scope_id").
		From("resources").
		Where(sq.Eq{"id": resourceID}).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&scopeID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}

	if !scopeID.Valid {
		return "", false, nil
	}

	var version ResourceVersion
	err = psql.Select("version_md5").
		From("resource_config_versions").
		Where(sq.Eq{"resource_config_scope_id": scopeID}).
		Where(sq.Expr("version_md5 NOT IN (SELECT version_md5 FROM resource_disabled_versions WHERE resource_id = ?)", resourceID)).
		OrderBy("check_order DESC").
		Limit(1).
		RunWith(tx).
		QueryRowContext(ctx).
		Scan(&version)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}

	return version, true, nil
}

// Migrates a single build into the successful build outputs table.
func (versions VersionsDB) migrateSingle(ctx context.Context, buildID int) (string, error) {
	var outputs string
	err := versions.conn.QueryRowContext(ctx, `
		WITH builds_to_migrate AS (
			UPDATE builds
			SET needs_v6_migration = false
			WHERE id = $1
		)
			INSERT INTO successful_build_outputs (
				SELECT b.id, b.job_id, json_object_agg(sp.resource_id, sp.v), b.rerun_of
				FROM builds b
				JOIN (
					SELECT build_id, resource_id, json_agg(version_md5) AS v
					FROM (
						(
							SELECT build_id, resource_id, version_md5 FROM build_resource_config_version_outputs o WHERE o.build_id = $1
						)
						UNION ALL
						(
							SELECT build_id, resource_id, version_md5 FROM build_resource_config_version_inputs i WHERE i.build_id = $1
						)
				) AS agg GROUP BY build_id, resource_id) sp ON sp.build_id = b.id
				WHERE b.id = $1
				GROUP BY b.id, b.job_id, b.rerun_of
			) ON CONFLICT (build_id) DO UPDATE SET outputs = EXCLUDED.outputs RETURNING outputs`, buildID).
		Scan(&outputs)
	if err != nil {
		return "", err
	}

	return outputs, nil
}

type pagedBuild struct {
	id      int
	rerunOf sql.NullInt64
}

type PaginatedBuilds struct {
	builder sq.SelectBuilder
	column  string

	unusedBuilds bool
	builds       []pagedBuild
	offset       int

	jobID int

	limitRows int
	conn      Conn
}

func (bs *PaginatedBuilds) Next(ctx context.Context) (int, bool, error) {
	if bs.offset+1 > len(bs.builds) {
		for {
			builder := bs.builder

			if len(bs.builds) > 0 {
				pageBoundary := bs.builds[len(bs.builds)-1]

				if pageBoundary.rerunOf.Valid {
					builder = bs.builder.Where(sq.Or{
						sq.Expr("COALESCE(rerun_of, "+bs.column+") < ?", pageBoundary.rerunOf.Int64),
						sq.And{
							sq.Eq{"rerun_of": pageBoundary.rerunOf.Int64},
							sq.Lt{bs.column: pageBoundary.id},
						},
					})
				} else {
					builder = bs.builder.Where(
						sq.Expr("COALESCE(rerun_of, "+bs.column+") < ?", pageBoundary.id),
					)
				}
			}

			rows, err := builder.
				Limit(uint64(bs.limitRows)).
				RunWith(bs.conn).
				QueryContext(ctx)
			if err != nil {
				return 0, false, err
			}

			builds := []pagedBuild{}
			for rows.Next() {
				var build pagedBuild
				err = rows.Scan(&build.id, &build.rerunOf)
				if err != nil {
					return 0, false, err
				}

				builds = append(builds, build)
			}

			if len(builds) == 0 {
				migrated, err := bs.migrateLimit(ctx)
				if err != nil {
					return 0, false, err
				}

				if !migrated {
					return 0, false, nil
				}
			} else {
				bs.builds = builds
				bs.offset = 0
				bs.unusedBuilds = false
				break
			}
		}
	}

	build := bs.builds[bs.offset]
	bs.offset++

	return build.id, true, nil
}

func (bs *PaginatedBuilds) HasNext() bool {
	return bs.unusedBuilds && len(bs.builds)-bs.offset+1 > 0
}

// Migrates a fixed limit of builds for a job
func (bs *PaginatedBuilds) migrateLimit(ctx context.Context) (bool, error) {
	buildsToMigrateQueryBuilder := psql.Select("id", "job_id", "rerun_of").
		From("builds").
		Where(sq.Eq{
			"job_id":             bs.jobID,
			"needs_v6_migration": true,
			"status":             "succeeded",
		}).
		OrderBy("COALESCE(rerun_of, id) DESC, id DESC").
		Limit(uint64(bs.limitRows))

	return migrate(ctx, bs.conn, buildsToMigrateQueryBuilder)
}

func migrate(ctx context.Context, conn Conn, buildsToMigrateQueryBuilder sq.SelectBuilder) (bool, error) {
	buildsToMigrateQuery, params, err := buildsToMigrateQueryBuilder.ToSql()
	if err != nil {
		return false, err
	}

	results, err := conn.ExecContext(ctx, `
		WITH builds_to_migrate AS (`+buildsToMigrateQuery+`), migrated_outputs AS (
			INSERT INTO successful_build_outputs (
				SELECT bm.id, bm.job_id, json_object_agg(sp.resource_id, sp.v), bm.rerun_of
				FROM builds_to_migrate bm
				JOIN (
					SELECT build_id, resource_id, json_agg(version_md5) AS v
					FROM (
						(
							SELECT build_id, resource_id, version_md5 FROM build_resource_config_version_outputs o JOIN builds_to_migrate bm ON bm.id = o.build_id
						)
						UNION ALL
						(
							SELECT build_id, resource_id, version_md5 FROM build_resource_config_version_inputs i JOIN builds_to_migrate bm ON bm.id = i.build_id
						)
				) AS agg GROUP BY build_id, resource_id) sp ON sp.build_id = bm.id
				GROUP BY bm.id, bm.job_id, bm.rerun_of
			) ON CONFLICT (build_id) DO NOTHING
		)
		UPDATE builds
		SET needs_v6_migration = false
		WHERE id IN (SELECT id FROM builds_to_migrate)`, params...)
	if err != nil {
		return false, err
	}

	rowsAffected, err := results.RowsAffected()
	if err != nil {
		return false, err
	}

	if rowsAffected == 0 {
		return false, nil
	}

	return true, nil
}
