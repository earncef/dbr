package dbr

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gocraft/dbr/dialect"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

//
// Test helpers
//

var (
	mysqlDSN    = os.Getenv("DBR_TEST_MYSQL_DSN")
	postgresDSN = os.Getenv("DBR_TEST_POSTGRES_DSN")
	sqlite3DSN  = ":memory:"
)

func createSession(driver, dsn string) *Session {
	conn, err := Open(driver, dsn, nil)
	if err != nil {
		panic(err)
	}
	return conn.NewSession(nil)
}

var (
	mysqlSession          = createSession("mysql", mysqlDSN)
	postgresSession       = createSession("postgres", postgresDSN)
	postgresBinarySession = createSession("postgres", postgresDSN+"&binary_parameters=yes")
	sqlite3Session        = createSession("sqlite3", sqlite3DSN)

	// all test sessions should be here
	testSession = []*Session{mysqlSession, postgresSession, sqlite3Session}
)

type dbrPerson struct {
	Id    int64
	Name  string
	Email string
}

type nullTypedRecord struct {
	Id         int64
	StringVal  NullString
	Int64Val   NullInt64
	Float64Val NullFloat64
	TimeVal    NullTime
	BoolVal    NullBool
}

func reset(t *testing.T, sess *Session) {
	var autoIncrementType string
	switch sess.Dialect {
	case dialect.MySQL:
		autoIncrementType = "serial PRIMARY KEY"
	case dialect.PostgreSQL:
		autoIncrementType = "serial PRIMARY KEY"
	case dialect.SQLite3:
		autoIncrementType = "integer PRIMARY KEY"
	}
	for _, v := range []string{
		`DROP TABLE IF EXISTS dbr_people`,
		fmt.Sprintf(`CREATE TABLE dbr_people (
			id %s,
			name varchar(255) NOT NULL,
			email varchar(255)
		)`, autoIncrementType),

		`DROP TABLE IF EXISTS null_types`,
		fmt.Sprintf(`CREATE TABLE null_types (
			id %s,
			string_val varchar(255) NULL,
			int64_val integer NULL,
			float64_val float NULL,
			time_val timestamp NULL ,
			bool_val bool NULL
		)`, autoIncrementType),
	} {
		_, err := sess.Exec(v)
		assert.NoError(t, err)
	}
}

func TestBasicCRUD(t *testing.T) {
	for _, sess := range testSession {
		reset(t, sess)

		jonathan := dbrPerson{
			Name:  "jonathan",
			Email: "jonathan@uservoice.com",
		}
		insertColumns := []string{"name", "email"}
		if sess.Dialect == dialect.PostgreSQL {
			jonathan.Id = 1
			insertColumns = []string{"id", "name", "email"}
		}
		// insert
		result, err := sess.InsertInto("dbr_people").Columns(insertColumns...).Record(&jonathan).Exec()
		assert.NoError(t, err)

		rowsAffected, err := result.RowsAffected()
		assert.NoError(t, err)
		assert.EqualValues(t, 1, rowsAffected)

		assert.True(t, jonathan.Id > 0)
		// select
		var people []dbrPerson
		count, err := sess.Select("*").From("dbr_people").Where(Eq("id", jonathan.Id)).Load(&people)
		assert.NoError(t, err)
		if assert.Equal(t, 1, count) {
			assert.Equal(t, jonathan.Id, people[0].Id)
			assert.Equal(t, jonathan.Name, people[0].Name)
			assert.Equal(t, jonathan.Email, people[0].Email)
		}

		// select id
		ids, err := sess.Select("id").From("dbr_people").ReturnInt64s()
		assert.NoError(t, err)
		assert.Equal(t, 1, len(ids))

		// select id limit
		ids, err = sess.Select("id").From("dbr_people").Limit(1).ReturnInt64s()
		assert.NoError(t, err)
		assert.Equal(t, 1, len(ids))

		// update
		result, err = sess.Update("dbr_people").Where(Eq("id", jonathan.Id)).Set("name", "jonathan1").Exec()
		assert.NoError(t, err)

		rowsAffected, err = result.RowsAffected()
		assert.NoError(t, err)
		assert.EqualValues(t, 1, rowsAffected)

		var n NullInt64
		sess.Select("count(*)").From("dbr_people").Where("name = ?", "jonathan1").LoadOne(&n)
		assert.EqualValues(t, 1, n.Int64)

		// delete
		result, err = sess.DeleteFrom("dbr_people").Where(Eq("id", jonathan.Id)).Exec()
		assert.NoError(t, err)

		rowsAffected, err = result.RowsAffected()
		assert.NoError(t, err)
		assert.EqualValues(t, 1, rowsAffected)

		// select id
		ids, err = sess.Select("id").From("dbr_people").ReturnInt64s()
		assert.NoError(t, err)
		assert.Equal(t, 0, len(ids))
	}
}

func TestTimeout(t *testing.T) {
	for _, sess := range testSession {
		reset(t, sess)

		// session op timeout
		sess.Timeout = time.Nanosecond
		var people []dbrPerson
		_, err := sess.Select("*").From("dbr_people").Load(&people)
		assert.EqualValues(t, context.DeadlineExceeded, err)

		_, err = sess.InsertInto("dbr_people").Columns("name", "email").Values("test", "test@test.com").Exec()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		_, err = sess.Update("dbr_people").Set("name", "test1").Exec()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		_, err = sess.DeleteFrom("dbr_people").Exec()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		// tx timeout
		_, err = sess.Begin()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		// tx op timeout
		sess.Timeout = 0
		tx, err := sess.Begin()
		assert.NoError(t, err)
		defer tx.RollbackUnlessCommitted()
		tx.Timeout = time.Nanosecond

		_, err = tx.Select("*").From("dbr_people").Load(&people)
		assert.EqualValues(t, context.DeadlineExceeded, err)

		_, err = tx.InsertInto("dbr_people").Columns("name", "email").Values("test", "test@test.com").Exec()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		_, err = tx.Update("dbr_people").Set("name", "test1").Exec()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		_, err = tx.DeleteFrom("dbr_people").Exec()
		assert.EqualValues(t, context.DeadlineExceeded, err)

		// tx commit timeout
		sess.Timeout = time.Second
		tx, err = sess.Begin()
		assert.NoError(t, err)
		defer tx.RollbackUnlessCommitted()
		time.Sleep(2 * time.Second)
		err = tx.Commit()
		assert.EqualValues(t, sql.ErrTxDone, err)
	}
}
