// Package scripts 存放需要下发到目标服务器的运维脚本。
package scripts

import _ "embed"

// NginxAnalyzer 是 nginx_analyzer.sh 的内容，编译进后端二进制。
// 平台以仓库内该文件为唯一真源：调用前会比对目标服务器上的副本 md5，
// 缺失或不一致时自动覆盖同步，保证版本不漂移。
//
//go:embed nginx_analyzer.sh
var NginxAnalyzer string

// NginxLogRotate 是 nginx_log_rotate.sh 的内容，编译进后端二进制。
// 平台每次分析前按 md5 同步到目标服务器的 /usr/local/sbin/nginx_log_rotate.sh，
// 让服务器原有的 cron 切割任务自动接入「备份 + 预生成 + 分析」闭环；
// 首次覆盖前会留一份 .bak 便于回滚。
//
//go:embed nginx_log_rotate.sh
var NginxLogRotate string
