# Toolchain de cross-compilation Windows (mingw-w64) pour leo-agent.
#
# Usage :
#   cmake --toolchain cmake/mingw-toolchain.cmake -DTARGET_OS=windows \
#         -DOPENSSL_ROOT_DIR=<préfixe openssl-mingw> -B build-win
#   cmake --build build-win
#
# OpenSSL n'a pas de paquet mingw-w64 dans les dépôts Ubuntu — il doit être
# cross-compilé depuis les sources au préalable (voir la procédure "no-quic
# no-apps --cross-compile-prefix=x86_64-w64-mingw32-" documentée dans le plan
# de portage Windows), puis son préfixe d'installation passé via
# -DOPENSSL_ROOT_DIR sur la ligne de commande.
set(CMAKE_SYSTEM_NAME Windows)
set(CMAKE_SYSTEM_PROCESSOR x86_64)

set(TOOLCHAIN_PREFIX x86_64-w64-mingw32)

set(CMAKE_C_COMPILER   ${TOOLCHAIN_PREFIX}-gcc)
set(CMAKE_CXX_COMPILER ${TOOLCHAIN_PREFIX}-g++)
set(CMAKE_RC_COMPILER  ${TOOLCHAIN_PREFIX}-windres)

# OPENSSL_MINGW_PREFIX (variable cache, passée via -D) : préfixe d'install
# de l'OpenSSL cross-compilé — ajouté au root path pour que find_package
# (CMAKE_FIND_ROOT_PATH_MODE_* = ONLY ci-dessous) le trouve en plus du
# sysroot mingw système.
set(CMAKE_FIND_ROOT_PATH /usr/${TOOLCHAIN_PREFIX} ${OPENSSL_MINGW_PREFIX})
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_PACKAGE ONLY)
