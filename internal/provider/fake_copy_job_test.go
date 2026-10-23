package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// copyJobFakeFamily names the copy job family in every fake catalog.
const copyJobFakeFamily = "copy_job"

// copyJobFakeRole is the IAM role of the representative job.
const copyJobFakeRole = "arn:aws:iam::123456789012:role/loader"

var _ = registerFakeFamily(copyJobFakeFamily, func() fakeFamily { return &copyJobFake{} })

// copyJobFake emulates one copy job as SYS_COPY_JOB reports it; its character columns are padded with blanks.
type copyJobFake struct {
	// exists records whether the job exists.
	exists bool
	// name is the job name.
	name string
	// role is the IAM role ARN of the job's COPY command.
	role string
	// auto records AUTO ON.
	auto       bool
	definition string
}

// populate makes the representative job exist with AUTO OFF.
func (f *copyJobFake) populate() {
	f.exists, f.name, f.role, f.auto = true, "events_load", copyJobFakeRole, false
	f.definition, _ = createCopyJobStatement(copyJobLifecycleModel)
}

// copyJobFakeChar pads a value to a character column's width, as Redshift returns char(n).
func copyJobFakeChar(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-len(value)))
}

// copyJobFakeAfter returns the word following the given words in sql.
func copyJobFakeAfter(sql, words string) string {
	_, rest, found := strings.Cut(sql, " "+words+" ")
	if fields := strings.Fields(rest); found && len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// query answers SYS_COPY_JOB reads and applies COPY JOB statements; other SQL passes to the next family.
func (f *copyJobFake) query(_ *catalog, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	row := func() sqlclient.Row {
		isAuto := "0"
		if f.auto {
			isAuto = "1"
		}
		return sqlclient.Row{"job_name": copyJobFakeChar(f.name, 128), "iam_role": copyJobFakeChar(f.role, copyJobRoleLength), "is_auto": isAuto, "copy_query": f.definition}
	}
	switch {
	case strings.HasPrefix(sql, "SELECT * FROM sys_copy_job WHERE job_name = :name"):
		if !f.exists || parameters["name"] != f.name {
			return nil, true, nil
		}
		return []sqlclient.Row{row()}, true, nil
	case strings.HasPrefix(sql, "SELECT job_name, iam_role, is_auto FROM sys_copy_job ORDER BY job_name"):
		if !f.exists {
			return nil, true, nil
		}
		return []sqlclient.Row{row()}, true, nil
	case strings.HasPrefix(sql, "COPY JOB ALTER "):
		if !f.exists || copyJobFakeAfter(sql, "ALTER") != f.name {
			return nil, true, errors.New("copy job does not exist")
		}
		f.auto = strings.HasSuffix(sql, " AUTO ON")
	case strings.HasPrefix(sql, "COPY JOB DROP "):
		if !f.exists || copyJobFakeAfter(sql+" ", "DROP") != f.name {
			return nil, true, errors.New("copy job does not exist")
		}
		f.exists = false
	case strings.HasPrefix(sql, "COPY ") && strings.Contains(sql, " JOB CREATE "):
		if f.exists {
			return nil, true, errors.New("copy job already exists")
		}
		_, authorization, found := strings.Cut(sql, " IAM_ROLE ")
		if !found {
			return nil, true, fmt.Errorf("copy job without IAM_ROLE: %s", sql)
		}
		role, err := literal(authorization, 0)
		if err != nil {
			return nil, true, err
		}
		if !strings.HasSuffix(sql, " AUTO ON") && !strings.HasSuffix(sql, " AUTO OFF") {
			return nil, true, fmt.Errorf("copy job without explicit AUTO: %s", sql)
		}
		f.exists, f.name, f.role, f.auto = true, copyJobFakeAfter(sql, "JOB CREATE"), role, strings.HasSuffix(sql, " AUTO ON")
		f.definition = sql
	default:
		return nil, false, nil
	}
	return nil, true, nil
}
