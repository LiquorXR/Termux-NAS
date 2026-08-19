#!/data/data/com.termux/files/usr/bin/sh
# runit 服务脚本 —— 注册方式:
#
#   pkg install termux-services
#   mkdir -p $PREFIX/var/service/nasd/log
#   cp termux-service/nasd-run.sh $PREFIX/var/service/nasd/run
#   chmod +x $PREFIX/var/service/nasd/run
#   # 可选:日志目录 + 轮转
#   echo '#!/data/data/com.termux/files/usr/bin/sh' > $PREFIX/var/service/nasd/log/run
#   echo 'exec svlogd -tt $HOME/nas/data/logs' >> $PREFIX/var/service/nasd/log/run
#   chmod +x $PREFIX/var/service/nasd/log/run
#   sv-enable nasd        # 注册并开机自启(Termux:Boot)
#
# 说明:nasd 自身会把日志写到 data/logs/nasd.log;
# 若使用上方 svlogd 轮转,两者会并存,可按需二选一。

export NAS_ROOT="$HOME/nas"
exec "$HOME/nas/bin/nasd" -root "$NAS_ROOT"
