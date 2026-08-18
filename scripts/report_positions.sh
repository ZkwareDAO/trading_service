#!/bin/bash
# 仓位播报脚本 - 由 crontab 调度执行
# 用法:
#   ./scripts/report_positions.sh
#
# crontab 示例:
#   每小时整点:  0 * * * * /path/to/trading_service/scripts/report_positions.sh
#   每30分钟:    */30 * * * * /path/to/trading_service/scripts/report_positions.sh
#   每天早8点:   0 8 * * * /path/to/trading_service/scripts/report_positions.sh

set -euo pipefail

# 项目根目录（脚本所在目录的上级）
PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${PROJECT_DIR}/bin/exchange_position_reporter"
LOG_DIR="${PROJECT_DIR}/logs"

# 确保日志目录存在
mkdir -p "${LOG_DIR}"

# 检查二进制是否存在
if [ ! -f "${BIN}" ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: ${BIN} not found, run 'make build' first" >> "${LOG_DIR}/reporter.log"
    exit 1
fi

# 执行播报
cd "${PROJECT_DIR}" && "${BIN}" >> "${LOG_DIR}/reporter.log" 2>&1
