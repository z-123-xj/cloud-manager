package models

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"cloud-manager-backend/pkg/logger"
	"cloud-manager-backend/pkg/utils"
)

// InitSystem 执行系统初始化（配合启动参数 init 子命令）：
//  1. 先导入 scripts/init_db.sql 中的建表语句（CREATE TABLE IF NOT EXISTS），确保标准表存在；
//  2. 比对现有表结构与 init_db.sql 中的“标准结构”，补齐/强制对齐：
//     - 受保护表（见 protectedTables）：只补缺失的列/索引，破坏性差异（多余列、类型不一致、多余/不一致索引）
//     仅打印告警，绝不自动删除/修改，避免数据丢失；
//     - 可重建表（每日同步/种子数据）：自动补齐缺失列/索引、删除多余索引（不丢数据），
//     若遇到多余列或列类型不一致等破坏性差异，则整表强制重建以对齐标准（数据由每日同步/种子恢复）；
//  3. 最后导入种子数据（INSERT IGNORE），确保即便上一步被强制重建的表也已重新灌入种子；
//  4. 确保至少存在一个管理员账号，否则按传入的账号/密码创建 admin。
//
// 该操作是幂等的：可重复执行；init_db.sql 只维护标准表结构，历史缺口由本函数的结构比对逻辑统一补齐，
// 无需在 SQL 中为每个缺口手写 ALTER。
func InitSystem(sqlPath, adminUser, adminPass string) error {
	// 1. 先确保标准表存在（CREATE TABLE IF NOT EXISTS，幂等）
	logger.Info("init", "开始导入标准表结构", "sql", sqlPath)
	if err := importCreateTables(sqlPath); err != nil {
		return fmt.Errorf("导入表结构失败: %w", err)
	}

	// 2. 比对现有表结构与标准结构，补齐/强制对齐
	if err := syncSchemaColumns(sqlPath); err != nil {
		return fmt.Errorf("校验表结构失败: %w", err)
	}
	logger.Info("init", "数据库结构校验完成（缺失已补齐，破坏性差异已按策略处理）")

	// 3. 最后灌种子（确保被强制重建的表也已重新灌入种子数据）
	if err := importSeeds(sqlPath); err != nil {
		return fmt.Errorf("导入种子数据失败: %w", err)
	}

	// 4. 确保存在管理员账号
	if err := ensureAdmin(adminUser, adminPass); err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	return nil
}

// CheckSchemaReady 同步前校验数据库结构是否与 scripts/init_db.sql 一致。
// 只检查“缺失”（缺表/缺列）——这些会直接导致同步写库失败（Unknown column / table doesn't exist）。
// 一旦发现缺失，返回带修复指引的错误，让调用方在同步前中止；读不到/解析不了 SQL 时降级放行，不误伤正常同步。
func CheckSchemaReady(sqlPath string) error {
	if DB == nil {
		return fmt.Errorf("数据库未初始化")
	}
	content, err := os.ReadFile(sqlPath)
	if err != nil {
		return nil // 取不到 SQL 文件则不阻塞同步
	}
	std, err := parseStandardSchema(string(content))
	if err != nil || len(std) == 0 {
		return nil
	}
	dbName := currentDatabase()
	var missingTables, missingCols []string
	for table, def := range std {
		exists, err := tableExists(dbName, table)
		if err != nil {
			return nil // 查询异常不阻塞
		}
		if !exists {
			missingTables = append(missingTables, table)
			continue
		}
		actual, err := actualColumns(dbName, table)
		if err != nil {
			return nil
		}
		for _, c := range def.columns {
			if _, ok := actual[c.name]; !ok {
				missingCols = append(missingCols, table+"."+c.name)
			}
		}
	}
	if len(missingTables) == 0 && len(missingCols) == 0 {
		return nil
	}
	msg := "数据库结构与代码不一致，同步已中止。请先在服务器执行 ./devops init 修复数据库后，再重新同步。"
	if len(missingTables) > 0 {
		msg += " 缺少表: " + strings.Join(missingTables, ", ") + "。"
	}
	if len(missingCols) > 0 {
		msg += " 缺少列: " + strings.Join(missingCols, ", ") + "。"
	}
	return fmt.Errorf("%s", msg)
}

// protectedTables 需要严格保留数据的表：只做“增加”操作（补列/补索引），绝不 DROP/MODIFY，避免数据丢失。
// 包含：用户管理(users)、云平台账号(cloud_accounts)，以及无法由每日同步恢复的用户配置表
// （账号地域绑定、堡垒机凭证/授权、用户组）。其余表的数据由每日同步或种子重新生成，可在结构不一致时强制对齐。
// 如需调整保护范围，直接增删此 map 即可。
var protectedTables = map[string]bool{
	"users":                    true,
	"cloud_accounts":           true,
	"bastion_credentials":      true,
	"bastion_group_assets":     true,
	"bastion_group_credentials": true,
	"user_groups":              true,
	"user_group_members":       true,
	"batch_tasks":              true,
	"batch_task_results":       true,
}

// columnDef 表示标准表结构中解析出的一列：列名 + 列定义（列名之后的完整定义，
// 例如 "VARCHAR(50) NOT NULL DEFAULT '' COMMENT '...'"）。
type columnDef struct {
	name string
	def  string
}

// indexDef 表示标准表结构中解析出的一个索引。
type indexDef struct {
	name    string
	columns []string
	unique  bool
}

// tableSchema 一张标准表的完整结构（列 + 索引 + 重建用的建表语句）。
type tableSchema struct {
	columns   []columnDef
	indexes   []indexDef
	createSQL string
}

// colMismatch 列类型不一致的差异项。
type colMismatch struct {
	name   string
	std    string
	actual string
}

// createTableRe 匹配 init_db.sql 中的 CREATE TABLE IF NOT EXISTS `表名` ( ... ) ENGINE... 语句，
// 捕获：表名、表定义体（括号内内容）、ENGINE 子句（用于整表重建）。
var createTableRe = regexp.MustCompile("(?is)CREATE\\s+TABLE\\s+IF\\s+NOT\\s+EXISTS\\s+`(\\w+)`\\s*\\((.*?)\\)\\s*(ENGINE[^;]*)")

// colTypeRe 从列定义中提取类型片段（类型词 + 可选精度 + 可选 unsigned/zerofill），用于与 information_schema.COLUMN_TYPE 比对。
var colTypeRe = regexp.MustCompile("(?i)^([a-z]+)(?:\\s*\\(([^)]*)\\))?(?:\\s+(unsigned|zerofill|signed))?")

// importCreateTables 仅执行 init_db.sql 中的建表语句（CREATE TABLE）。
func importCreateTables(path string) error {
	creates, _, err := readSchemaStatements(path)
	if err != nil {
		return err
	}
	for _, stmt := range creates {
		if err := DB.Exec(stmt).Error; err != nil {
			return fmt.Errorf("执行建表语句失败: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// importSeeds 仅执行 init_db.sql 中的种子语句（INSERT）。放在结构对齐之后，确保被重建的表也能重新灌入种子。
func importSeeds(path string) error {
	_, seeds, err := readSchemaStatements(path)
	if err != nil {
		return err
	}
	for _, stmt := range seeds {
		if err := DB.Exec(stmt).Error; err != nil {
			return fmt.Errorf("执行种子语句失败: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// readSchemaStatements 读取 SQL 文件，剔除 -- 行注释后按 ";" 切分，并按语句类型分类为建表/种子。
func readSchemaStatements(path string) (creates, seeds []string, err error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("读取初始化 SQL 失败(%s): %w", path, err)
	}
	for _, raw := range strings.Split(string(content), ";") {
		stmt := stripCommentAndTrim(raw)
		if stmt == "" {
			continue
		}
		up := strings.ToUpper(strings.TrimSpace(stmt))
		switch {
		case strings.HasPrefix(up, "CREATE TABLE"):
			creates = append(creates, stmt)
		case strings.HasPrefix(up, "INSERT"):
			seeds = append(seeds, stmt)
		default:
			// 其它语句（如 ALTER）也作为建表阶段执行，保持兼容
			creates = append(creates, stmt)
		}
	}
	return creates, seeds, nil
}

// stripCommentAndTrim 逐行剔除 -- 行注释并去除首尾空白。
func stripCommentAndTrim(raw string) string {
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// syncSchemaColumns 解析标准表结构（init_db.sql 的 CREATE TABLE）并与数据库现有表结构比对，按受保护/可重建策略补齐或强制对齐。
func syncSchemaColumns(sqlPath string) error {
	content, err := os.ReadFile(sqlPath)
	if err != nil {
		return fmt.Errorf("读取初始化 SQL 失败(%s): %w", sqlPath, err)
	}

	std, err := parseStandardSchema(string(content))
	if err != nil {
		return err
	}
	if len(std) == 0 {
		return nil
	}

	dbName := currentDatabase()
	for table, def := range std {
		exists, err := tableExists(dbName, table)
		if err != nil {
			return err
		}
		if !exists {
			// 表刚由 CREATE TABLE IF NOT EXISTS 创建，结构已完整，无需比对
			continue
		}

		actualCols, err := actualColumns(dbName, table)
		if err != nil {
			return err
		}
		actualIdx, err := actualIndexes(dbName, table)
		if err != nil {
			return err
		}

		missingCols, extraCols, typeMismatch := diffColumns(def.columns, actualCols)
		missingIdx, extraIdx, idxMismatch := diffIndexes(def.indexes, actualIdx)

		if protectedTables[table] {
			// 受保护表：只做“增加”，破坏性差异仅告警，绝不删除/修改
			for _, c := range missingCols {
				if err := addColumn(table, c); err != nil {
					return err
				}
			}
			for _, ix := range missingIdx {
				if err := addIndex(table, ix); err != nil {
					return err
				}
			}
			for _, c := range extraCols {
				logger.Warn("init", "受保护表存在标准外列，跳过（不删除，请人工确认）", "table", table, "column", c)
			}
			for _, m := range typeMismatch {
				logger.Warn("init", "受保护表列类型与标准不一致，跳过（不修改，请人工确认）", "table", table, "column", m.name, "standard", m.std, "actual", m.actual)
			}
			for _, ix := range extraIdx {
				logger.Warn("init", "受保护表存在标准外索引，跳过（不删除，请人工确认）", "table", table, "index", ix)
			}
			for _, ix := range idxMismatch {
				logger.Warn("init", "受保护表索引与标准不一致，跳过（不修改，请人工确认）", "table", table, "index", ix.name)
			}
			continue
		}

		// 可重建表：强制对齐
		// 1) 缺失的列/索引：安全补齐
		for _, c := range missingCols {
			if err := addColumn(table, c); err != nil {
				return err
			}
		}
		for _, ix := range missingIdx {
			if err := addIndex(table, ix); err != nil {
				return err
			}
		}
		// 2) 破坏性列差异（多余列/类型不一致）→ 整表强制重建（数据由每日同步/种子恢复）
		if len(extraCols) > 0 || len(typeMismatch) > 0 {
			if err := recreateTable(table, def.createSQL); err != nil {
				return err
			}
			continue
		}
		// 3) 多余的索引：安全删除（不丢数据）
		for _, ix := range extraIdx {
			if ix == "PRIMARY" {
				logger.Warn("init", "可重建表存在多余主键，跳过（不删除）", "table", table, "index", ix)
				continue
			}
			if err := dropIndex(table, ix); err != nil {
				return err
			}
		}
		// 4) 索引列不一致（同名不同列）→ 删除后按标准重建
		for _, ix := range idxMismatch {
			if ix.name == "PRIMARY" {
				logger.Warn("init", "可重建表主键与标准不一致，跳过（不修改）", "table", table, "index", ix.name)
				continue
			}
			if err := dropIndex(table, ix.name); err != nil {
				return err
			}
			if err := addIndex(table, ix); err != nil {
				return err
			}
		}
	}
	return nil
}

// parseStandardSchema 解析 init_db.sql 中所有 CREATE TABLE 的列、索引与重建语句。
func parseStandardSchema(content string) (map[string]tableSchema, error) {
	result := make(map[string]tableSchema)
	// 先剔除 -- 行注释，避免把注释里示例性的 CREATE TABLE（如 bastion_asset_bindings）误当作标准表，
	// 导致同步前结构校验(CheckSchemaReady)与结构对齐误报“缺少表”。
	content = stripCommentAndTrim(content)
	for _, m := range createTableRe.FindAllStringSubmatch(content, -1) {
		table := m[1]
		inner := m[2]
		enginePart := m[3]
		ts := tableSchema{
			createSQL: fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` (%s) %s", table, inner, enginePart),
		}
		for _, part := range splitTopLevel(inner) {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if strings.HasPrefix(p, "`") {
				// 列定义
				start := strings.Index(p, "`") + 1
				end := strings.Index(p[start:], "`") + start
				if end <= start {
					continue
				}
				ts.columns = append(ts.columns, columnDef{name: p[start:end], def: strings.TrimSpace(p[end+1:])})
			} else if name, cols, unique, ok := parseIndexDef(p); ok {
				// 索引/约束定义
				ts.indexes = append(ts.indexes, indexDef{name: name, columns: cols, unique: unique})
			}
		}
		result[table] = ts
	}
	return result, nil
}

// parseIndexDef 解析约束定义行，识别 PRIMARY KEY / UNIQUE KEY / UNIQUE INDEX / KEY / INDEX，返回索引名、列与是否唯一。
// 非索引约束（如 CONSTRAINT/FK）返回 ok=false 跳过。
func parseIndexDef(part string) (name string, columns []string, unique bool, ok bool) {
	p := strings.TrimSpace(part)
	up := strings.ToUpper(p)
	switch {
	case strings.HasPrefix(up, "PRIMARY KEY"):
		cols, ok := extractIndexColumns(p)
		if !ok {
			return "", nil, false, false
		}
		return "PRIMARY", cols, true, true
	case strings.HasPrefix(up, "UNIQUE KEY"), strings.HasPrefix(up, "UNIQUE INDEX"):
		name, cols := extractNamedIndex(p)
		return name, cols, true, true
	case strings.HasPrefix(up, "KEY "), strings.HasPrefix(up, "INDEX "):
		name, cols := extractNamedIndex(p)
		return name, cols, false, true
	default:
		return "", nil, false, false
	}
}

// extractNamedIndex 从 KEY/INDEX 定义中提取索引名（反引号包裹）与列列表。
func extractNamedIndex(p string) (string, []string) {
	name := ""
	if i := strings.Index(p, "`"); i >= 0 {
		if j := strings.Index(p[i+1:], "`"); j >= 0 {
			name = p[i+1 : i+1+j]
		}
	}
	cols, _ := extractIndexColumns(p)
	return name, cols
}

// extractIndexColumns 提取索引定义括号内的列名列表（反引号包裹）。
func extractIndexColumns(p string) ([]string, bool) {
	start := strings.Index(p, "(")
	if start < 0 {
		return nil, false
	}
	end := strings.Index(p[start:], ")")
	if end < 0 {
		return nil, false
	}
	inner := p[start+1 : start+end]
	var cols []string
	for _, c := range strings.Split(inner, ",") {
		c = strings.TrimSpace(c)
		c = strings.Trim(c, "`")
		if c != "" {
			cols = append(cols, c)
		}
	}
	return cols, true
}

// splitTopLevel 按顶层逗号切分（忽略括号内的逗号，如 decimal(10,2)、enum(...)，以及引号字符串内的逗号）。
func splitTopLevel(s string) []string {
	var parts []string
	depth := 0
	inStr := false
	var cur strings.Builder
	for _, r := range s {
		switch {
		case r == '\'':
			inStr = !inStr
			cur.WriteRune(r)
		case !inStr && r == '(':
			depth++
			cur.WriteRune(r)
		case !inStr && r == ')':
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case !inStr && r == ',':
			if depth == 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, cur.String())
	}
	return parts
}

// diffColumns 计算列差异：标准有/实际缺 → missing；实际有/标准无 → extra；同名但类型不一致 → mismatch。
func diffColumns(stdCols []columnDef, actual map[string]string) (missing []columnDef, extra []string, mismatch []colMismatch) {
	stdSet := make(map[string]columnDef, len(stdCols))
	for _, c := range stdCols {
		stdSet[c.name] = c
	}
	for _, c := range stdCols {
		a, ok := actual[c.name]
		if !ok {
			missing = append(missing, c)
			continue
		}
		if stdType := extractColumnType(c.def); stdType != "" && !strings.EqualFold(stdType, a) {
			mismatch = append(mismatch, colMismatch{name: c.name, std: stdType, actual: a})
		}
	}
	for name := range actual {
		if _, ok := stdSet[name]; !ok {
			extra = append(extra, name)
		}
	}
	return
}

// diffIndexes 计算索引差异：标准有/实际缺 → missing；实际有/标准无 → extra；同名但列或唯一性不一致 → mismatch。
func diffIndexes(stdIdx []indexDef, actual map[string]indexInfo) (missing []indexDef, extra []string, mismatch []indexDef) {
	stdSet := make(map[string]indexDef, len(stdIdx))
	for _, ix := range stdIdx {
		stdSet[ix.name] = ix
	}
	for _, ix := range stdIdx {
		a, ok := actual[ix.name]
		if !ok {
			missing = append(missing, ix)
			continue
		}
		if !sameColumns(a.columns, ix.columns) || a.unique != ix.unique {
			mismatch = append(mismatch, ix)
		}
	}
	for name := range actual {
		if _, ok := stdSet[name]; !ok {
			extra = append(extra, name)
		}
	}
	return
}

// sameColumns 比较两个列名切片是否完全一致（顺序敏感）。
func sameColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extractColumnType 从列定义中提取类型片段，归一化用于与 information_schema.COLUMN_TYPE 比对。
func extractColumnType(def string) string {
	m := colTypeRe.FindStringSubmatch(strings.TrimSpace(def))
	if m == nil {
		return ""
	}
	t := strings.ToLower(m[1])
	if m[2] != "" {
		t += "(" + m[2] + ")"
	}
	if m[3] != "" {
		t += " " + strings.ToLower(m[3])
	}
	return t
}

// addColumn 自动补齐缺失列（只增不改）。
func addColumn(table string, c columnDef) error {
	alter := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table, c.name, c.def)
	logger.Warn("init", "缺少标准列，自动补齐", "table", table, "column", c.name)
	if err := DB.Exec(alter).Error; err != nil {
		return fmt.Errorf("补齐列失败(%s.%s): %w\nSQL: %s", table, c.name, err, alter)
	}
	logger.Info("init", "已补齐缺失列", "table", table, "column", c.name)
	return nil
}

// addIndex 自动补齐缺失索引。
func addIndex(table string, ix indexDef) error {
	cols := quoteCols(ix.columns)
	var alter string
	if ix.name == "PRIMARY" {
		alter = fmt.Sprintf("ALTER TABLE `%s` ADD PRIMARY KEY (%s)", table, cols)
	} else if ix.unique {
		alter = fmt.Sprintf("ALTER TABLE `%s` ADD UNIQUE INDEX `%s` (%s)", table, ix.name, cols)
	} else {
		alter = fmt.Sprintf("ALTER TABLE `%s` ADD INDEX `%s` (%s)", table, ix.name, cols)
	}
	logger.Warn("init", "缺少标准索引，自动补齐", "table", table, "index", ix.name)
	if err := DB.Exec(alter).Error; err != nil {
		return fmt.Errorf("补齐索引失败(%s.%s): %w\nSQL: %s", table, ix.name, err, alter)
	}
	logger.Info("init", "已补齐缺失索引", "table", table, "index", ix.name)
	return nil
}

// dropIndex 删除多余/不一致索引（不丢数据）。
func dropIndex(table, name string) error {
	alter := fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`", table, name)
	logger.Warn("init", "存在标准外/不一致索引，强制删除以对齐标准", "table", table, "index", name)
	if err := DB.Exec(alter).Error; err != nil {
		return fmt.Errorf("删除索引失败(%s.%s): %w\nSQL: %s", table, name, err, alter)
	}
	logger.Info("init", "已删除索引", "table", table, "index", name)
	return nil
}

// recreateTable 整表强制重建以对齐标准（数据由每日同步/种子恢复）。
func recreateTable(table, createSQL string) error {
	logger.Warn("init", "表结构存在破坏性差异（多余列/类型不一致），强制重建表以对齐标准（数据将于下次同步恢复）", "table", table)
	if err := DB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", table)).Error; err != nil {
		return fmt.Errorf("删除表失败(%s): %w", table, err)
	}
	if err := DB.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("重建表失败(%s): %w\nSQL: %s", table, err, createSQL)
	}
	logger.Info("init", "已重建表以对齐标准结构", "table", table)
	return nil
}

// quoteCols 将列名列表用反引号包裹并以逗号连接。
func quoteCols(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = "`" + c + "`"
	}
	return strings.Join(quoted, ", ")
}

// currentDatabase 返回当前连接使用的数据库名。
func currentDatabase() string {
	var db string
	DB.Raw("SELECT DATABASE()").Scan(&db)
	return db
}

// tableExists 判断指定表是否存在于当前数据库。
func tableExists(dbName, table string) (bool, error) {
	var cnt int64
	if err := DB.Raw(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?",
		dbName, table,
	).Scan(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// actualColumns 返回表的现有列名 → 列类型映射（来自 information_schema.columns）。
func actualColumns(dbName, table string) (map[string]string, error) {
	var rows []map[string]interface{}
	if err := DB.Raw(
		"SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ORDINAL_POSITION",
		dbName, table,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		name, _ := r["COLUMN_NAME"].(string)
		typ, _ := r["COLUMN_TYPE"].(string)
		if name != "" {
			m[name] = typ
		}
	}
	return m, nil
}

// indexInfo 实际索引信息：列列表 + 是否唯一。
type indexInfo struct {
	columns []string
	unique  bool
}

// actualIndexes 返回表的现有索引名 → 索引信息映射（来自 information_schema.statistics）。
func actualIndexes(dbName, table string) (map[string]indexInfo, error) {
	var rows []map[string]interface{}
	if err := DB.Raw(
		"SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE FROM information_schema.statistics WHERE table_schema = ? AND table_name = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX",
		dbName, table,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]indexInfo, len(rows))
	for _, r := range rows {
		iname, _ := r["INDEX_NAME"].(string)
		cname, _ := r["COLUMN_NAME"].(string)
		unique := fmt.Sprint(r["NON_UNIQUE"]) == "0"
		info := m[iname]
		info.columns = append(info.columns, cname)
		info.unique = unique
		m[iname] = info
	}
	return m, nil
}

// ensureAdmin 确保至少存在一个管理员；若不存在则创建。
// 若库中已有其它用户但无管理员，仍补充一个管理员以便登录后台管理。
func ensureAdmin(username, password string) error {
	var adminCount int64
	DB.Model(&User{}).Where("role = ?", RoleAdmin).Count(&adminCount)
	if adminCount > 0 {
		logger.Info("init", "已存在管理员账号，跳过创建")
		return nil
	}

	hashed, err := utils.Encrypt(password)
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}

	admin := User{
		Username: username,
		Password: hashed,
		// example.com 是 RFC 2606 保留域名：格式合法（有顶级域名），且永远不会是真实邮箱。
		// 原 "@local" 无顶级域名，不符合邮箱格式规则，会导致后续编辑用户时前端/后端校验困惑。
		// 登录后可在用户管理中改成真实邮箱。
		Email:    username + "@example.com",
		Role:     RoleAdmin,
		RealName: "管理员",
		AddTime:  time.Now().Format("2006-01-02 15:04:05"),
	}
	if err := DB.Create(&admin).Error; err != nil {
		return fmt.Errorf("写入管理员失败: %w", err)
	}
	logger.Info("init", "已创建管理员账号", "username", username)
	return nil
}
