package sqli

// sqlKeywords is the set of SQL keywords across MySQL, PostgreSQL, MSSQL, SQLite.
var sqlKeywords = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"DROP": true, "ALTER": true, "CREATE": true, "TRUNCATE": true,
	"UNION": true, "ALL": true, "FROM": true, "WHERE": true,
	"AND": true, "OR": true, "NOT": true, "IN": true,
	"LIKE": true, "BETWEEN": true, "IS": true, "NULL": true,
	"TRUE": true, "FALSE": true, "AS": true, "ON": true,
	"JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true,
	"OUTER": true, "HAVING": true, "GROUP": true, "ORDER": true,
	"BY": true, "ASC": true, "DESC": true, "LIMIT": true,
	"OFFSET": true, "INTO": true, "VALUES": true, "SET": true,
	"TABLE": true, "DATABASE": true, "SCHEMA": true, "INDEX": true,
	"VIEW": true, "EXEC": true, "EXECUTE": true, "DECLARE": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"IF": true, "EXISTS": true, "BEGIN": true, "COMMIT": true,
	"ROLLBACK": true, "GRANT": true, "REVOKE": true,
	"OUTFILE": true, "DUMPFILE": true, "LOAD_FILE": true,
	"INFORMATION_SCHEMA": true, "CONCAT": true, "CHAR": true,
	"SLEEP": true, "BENCHMARK": true, "WAITFOR": true, "DELAY": true,
	"PG_SLEEP": true, "DBMS_PIPE": true,
}

// dangerousFunctions are SQL functions commonly used in attacks.
var dangerousFunctions = map[string]int{
	"SLEEP":        95,
	"BENCHMARK":    95,
	"WAITFOR":      95,
	"PG_SLEEP":     95,
	"LOAD_FILE":    90,
	"CHAR":         55,
	"CONCAT":       55,
	"HEX":          45,
	"UNHEX":        50,
	"CONV":         45,
	"EXTRACTVALUE": 80,
	"UPDATEXML":    80,
}
