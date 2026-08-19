# Toolchain de cross-compilation Windows (mingw-w64) pour leo-agent.
#
# Usage :
#   cmake --toolchain cmake/mingw-toolchain.cmake -DTARGET_OS=windows \
#         -DOPENSSL_ROOT_DIR=<préfixe openssl-mingw> -DOPENSSL_MINGW_PREFIX=<préfixe openssl-mingw> \
#         -DTURBOJPEG_MINGW_PREFIX=<préfixe libjpeg-turbo-mingw> -B build-win
#   cmake --build build-win
#
# OpenSSL et libjpeg-turbo n'ont pas de paquet mingw-w64 dans les dépôts
# Ubuntu — ils doivent être cross-compilés depuis les sources au préalable
# (OpenSSL : procédure "no-quic no-apps --cross-compile-prefix=
# x86_64-w64-mingw32-" documentée dans le plan de portage Windows ;
# libjpeg-turbo : CMake standard avec -DCMAKE_TOOLCHAIN_FILE=ce fichier,
# depuis SA PROPRE racine source — pas via add_subdirectory(), voir le
# commentaire sur libjpeg-turbo dans CMakeLists.txt), puis leurs préfixes
# d'installation respectifs passés sur la ligne de commande.
set(CMAKE_SYSTEM_NAME Windows)
set(CMAKE_SYSTEM_PROCESSOR x86_64)

set(TOOLCHAIN_PREFIX x86_64-w64-mingw32)

set(CMAKE_C_COMPILER   ${TOOLCHAIN_PREFIX}-gcc)
set(CMAKE_CXX_COMPILER ${TOOLCHAIN_PREFIX}-g++)
set(CMAKE_RC_COMPILER  ${TOOLCHAIN_PREFIX}-windres)

# OPENSSL_MINGW_PREFIX / TURBOJPEG_MINGW_PREFIX (variables cache, passées
# via -D) : préfixes d'install des dépendances cross-compilées — ajoutés au
# root path pour que find_package/find_path/find_library
# (CMAKE_FIND_ROOT_PATH_MODE_* = ONLY ci-dessous) les trouvent en plus du
# sysroot mingw système.
set(CMAKE_FIND_ROOT_PATH /usr/${TOOLCHAIN_PREFIX} ${OPENSSL_MINGW_PREFIX} ${TURBOJPEG_MINGW_PREFIX})
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_PACKAGE ONLY)
