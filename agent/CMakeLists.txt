cmake_minimum_required(VERSION 3.20)
project(leo-agent VERSION 1.0.0 LANGUAGES C)

# ─── Standard C11 ──────────────────────────────────────────────────────────
set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_C_EXTENSIONS OFF)

# CMAKE_C_EXTENSIONS OFF passe -std=c11 (pas -std=gnu11) : glibc masque alors
# nanosleep/sigaction/clock_gettime/popen/strcasecmp/... sans ceci.
# _GNU_SOURCE est requis en plus pour pthread_timedjoin_np (extension glibc,
# hors POSIX, utilisée par connection.c — implique une dépendance à glibc).
add_definitions(-D_POSIX_C_SOURCE=200809L -D_DEFAULT_SOURCE -D_GNU_SOURCE)

# ─── Options de build ────────────────────────────────────────────────────────
option(TARGET_OS "Système cible (linux|windows|macos)" "linux")

# ─── Sources communes ─────────────────────────────────────────────────────────
set(AGENT_COMMON_SOURCES
    src/main.c
    src/agent.c
    src/config.c
    src/enroll.c
    src/logger.c
    src/protocol.c
    src/connection.c
    src/metrics.c
    src/http_url.c
    src/file_transfer.c
    src/remote_desktop.c
    third_party/cjson/cJSON.c
)

# ─── Sources spécifiques à la plateforme ─────────────────────────────────────
if(TARGET_OS STREQUAL "linux" OR CMAKE_SYSTEM_NAME STREQUAL "Linux")
    list(APPEND AGENT_COMMON_SOURCES
        platform/linux/metrics_linux.c
        platform/linux/service_linux.c
        platform/linux/executor_linux.c
        platform/linux/crypto_linux.c
        platform/linux/inventory_linux.c
        platform/linux/patches_linux.c
        platform/linux/capture_linux.c
        platform/linux/input_linux.c
    )
    add_definitions(-DLEO_PLATFORM_LINUX)

elseif(TARGET_OS STREQUAL "windows" OR CMAKE_SYSTEM_NAME STREQUAL "Windows")
    list(APPEND AGENT_COMMON_SOURCES
        platform/windows/metrics_win.c
        platform/windows/service_win.c
        platform/windows/executor_win.c
        platform/windows/crypto_win.c
        platform/windows/inventory_win.c
        platform/windows/patches_win.c
    )
    add_definitions(-DLEO_PLATFORM_WINDOWS)
    # Cible Windows 7+ (0x0601) : nécessaire pour que <iphlpapi.h> déclare
    # MIB_IF_TABLE2/GetIfTable2 (Vista+) utilisés par metrics_win.c — le
    # défaut mingw-w64 est bien plus ancien. libwebsockets définit déjà la
    # même valeur en interne (CMakeLists.txt:1248) mais uniquement pour ses
    # propres sources ; la cible leo-agent (définie dans CE fichier, avant
    # l'add_subdirectory plus bas) a besoin de sa propre définition.
    add_definitions(-DWINVER=0x0601 -D_WIN32_WINNT=0x0601)
    # Shim de casse pour la cross-compilation depuis un FS sensible à la
    # casse (Linux) — voir cmake/mingw-case-shims/Psapi.h. Sans effet sur un
    # FS insensible à la casse (Windows natif) : ajoute juste un répertoire
    # de plus à l'include path. Injecté via CMAKE_C_FLAGS (pas
    # include_directories()) pour s'appliquer aussi à libwebsockets,
    # vendored et ajouté plus bas par add_subdirectory().
    set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -I${CMAKE_CURRENT_SOURCE_DIR}/cmake/mingw-case-shims")
    # libwebsockets détecte correctement pthread.h disponible sous mingw-w64
    # (variante posix, voir Threads::Threads plus bas) et active du code
    # POSIX-threads (ex: lib/core-net/txpacer.c) en s'attendant à ce que
    # <pthread.h> soit déjà inclus par un header amont — vrai sous Unix, pas
    # sous sa propre chaîne de headers plat/windows (pensée pour ne PAS avoir
    # pthread.h). -include force son inclusion dans chaque unité de
    # compilation ; sans effet sur nos propres fichiers qui l'incluent déjà
    # explicitement (garde d'inclusion standard).
    set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -include pthread.h")

elseif(TARGET_OS STREQUAL "macos" OR CMAKE_SYSTEM_NAME STREQUAL "Darwin")
    list(APPEND AGENT_COMMON_SOURCES
        platform/macos/metrics_macos.c
        platform/macos/service_macos.c
        platform/macos/executor_macos.c
        platform/macos/crypto_macos.c
    )
    add_definitions(-DLEO_PLATFORM_MACOS)
endif()

# ─── Exécutable ───────────────────────────────────────────────────────────────
add_executable(leo-agent ${AGENT_COMMON_SOURCES})

# Flags de sécurité et optimisation : ciblés sur leo-agent (target_compile_options),
# pas add_compile_options() global — sinon ces flags stricts (-Wpedantic...) fuient
# dans le build de libwebsockets vendored, qui n'est pas propre sous cette rigueur.
target_compile_options(leo-agent PRIVATE
    -Wall -Wextra -Wpedantic
    -Wformat=2 -Wformat-security
    -fstack-protector-strong
    -D_FORTIFY_SOURCE=2
    $<$<CONFIG:Release>:-O2>
    $<$<CONFIG:Debug>:-O0>
    $<$<CONFIG:Debug>:-g>
    $<$<CONFIG:Debug>:-fsanitize=address,undefined>
)
target_link_options(leo-agent PRIVATE
    $<$<CONFIG:Debug>:-fsanitize=address,undefined>
)
if(NOT WIN32)
    # RELRO complet : flags GNU-ld/ELF, absents sous Windows (PE/COFF, pas de
    # segment GNU_RELRO) — un -Wl,-z,relro y ferait échouer l'édition de liens.
    target_link_options(leo-agent PRIVATE -Wl,-z,relro -Wl,-z,now)
endif()

target_include_directories(leo-agent PRIVATE
    include
    src
    third_party/cjson
    third_party/libwebsockets/include
    # lws_config.h est généré par le configure_file() de libwebsockets dans
    # son propre dossier de build, pas dans include/ source.
    ${CMAKE_CURRENT_BINARY_DIR}/third_party/libwebsockets
)

# ─── Dépendances externes ─────────────────────────────────────────────────────

# libwebsockets (build statique vendored)
# L'agent n'a besoin que de WSS classique (OpenSSL, déjà requis ci-dessous).
# LWS_WITH_HTTP3 est ON par défaut dans libwebsockets 5.x et force GnuTLS
# (OpenSSL n'est pas éligible pour son détecteur de backend HTTP/3) : on le
# désactive pour rester sur OpenSSL et éviter une dépendance GnuTLS inutile.
set(LWS_WITH_HTTP3 OFF CACHE BOOL "" FORCE)
# libwebsockets active -Werror par défaut (DISABLE_WERROR=OFF) sur son propre
# code : on ne veut pas qu'une future version de gcc casse notre build à
# cause d'un warning dans du code qu'on ne maintient pas.
set(DISABLE_WERROR ON CACHE BOOL "" FORCE)
# Sous Windows, LWS_WITH_SCHANNEL est ON par défaut (SSL natif Windows) sauf
# si un autre backend TLS est déjà sélectionné — mais on veut rester sur
# OpenSSL (même raisonnement que LWS_WITH_HTTP3 ci-dessus : une seule
# dépendance TLS à maintenir/auditer sur toutes les plateformes). Sans ce
# FORCE, Schannel entraîne aussi la compilation de fonctionnalités qu'on
# n'utilise pas (CGI/spawn de sous-processus par libwebsockets lui-même —
# l'agent a sa propre implémentation dans platform/windows/executor_win.c).
set(LWS_WITH_SCHANNEL OFF CACHE BOOL "" FORCE)
# Les "minimal examples" sont ON par défaut (LWS_WITH_MINIMAL_EXAMPLES) et
# construisent ~80 binaires de démo qu'on n'utilise jamais — coûteux à
# builder, et l'un d'eux (api-test-ws-h2-txcredit, allocation de port de
# test) échoue à créer son fichier de verrou en cross-compilation. On n'a
# besoin que de la bibliothèque "websockets" elle-même, jamais des exemples.
set(LWS_WITH_MINIMAL_EXAMPLES OFF CACHE BOOL "" FORCE)
add_subdirectory(third_party/libwebsockets EXCLUDE_FROM_ALL)
# La cible statique s'appelle "websockets" dans libwebsockets >= 4.x
# ("websockets_static" est un nom d'une ancienne version, jamais buildé ici).
target_link_libraries(leo-agent PRIVATE websockets)

# libjpeg-turbo — encodage JPEG des frames de bureau à distance (voir
# src/remote_desktop.c). PAS vendored : son propre CMakeLists.txt refuse
# explicitement add_subdirectory() ("The libjpeg-turbo build system cannot
# be integrated into another build system using add_subdirectory() [...]
# Please do not file bug reports [...] regarding this" — politique amont
# délibérée, pas une limitation de configuration de notre côté). Paquet
# système à la place, même schéma que OpenSSL ci-dessous (find_package sur
# une bibliothèque système sous Linux) : `apt install libturbojpeg0-dev`.
# Aucun module CMake officiel FindTurboJPEG n'existe, d'où ce find_path/
# find_library manuel. La cross-compilation Windows (phase suivante du
# portage, voir platform/windows/) n'a pas ce paquet dans les dépôts Ubuntu
# et nécessitera un cross-build depuis les sources — même situation et même
# solution que celle déjà appliquée à OpenSSL pour Windows (voir
# cmake/mingw-toolchain.cmake et la mémoire du projet sur le portage
# Windows), pas encore mise en place ici.
find_path(TurboJPEG_INCLUDE_DIR NAMES turbojpeg.h)
find_library(TurboJPEG_LIBRARY NAMES turbojpeg)
if(NOT TurboJPEG_INCLUDE_DIR OR NOT TurboJPEG_LIBRARY)
    message(FATAL_ERROR "libturbojpeg introuvable — requis pour le bureau à distance "
                         "(voir src/remote_desktop.c). Sous Ubuntu/Debian : "
                         "sudo apt install libturbojpeg0-dev")
endif()
target_include_directories(leo-agent PRIVATE ${TurboJPEG_INCLUDE_DIR})
target_link_libraries(leo-agent PRIVATE ${TurboJPEG_LIBRARY})

# Threads POSIX
find_package(Threads REQUIRED)
target_link_libraries(leo-agent PRIVATE Threads::Threads)

# OpenSSL (nécessaire pour libwebsockets)
find_package(OpenSSL REQUIRED)
target_link_libraries(leo-agent PRIVATE OpenSSL::SSL OpenSSL::Crypto)

# Bibliothèques d'import Windows : ws2_32 (Winsock, requis par libwebsockets
# et GetComputerNameA), crypt32/bcrypt (dépendances d'OpenSSL sur Windows),
# advapi32 (Service Control Manager + registre + privilège SE_SHUTDOWN_NAME,
# voir patches_win.c), iphlpapi (GetIfTable2), ole32/oleaut32 (API COM
# Windows Update Agent — CoCreateInstance/BSTR/DECIMAL, voir patches_win.c ;
# les GUID CLSID_UpdateSession & co. sont définis dans patches_win.c lui-même
# via INITGUID, pas besoin d'une bibliothèque d'import wuapi dédiée).
if(WIN32)
    target_link_libraries(leo-agent PRIVATE ws2_32 crypt32 bcrypt advapi32 iphlpapi ole32 oleaut32)
endif()

# X11 + extensions Xext (XShm, voir capture_linux.c) et Xtst (XTest, voir
# input_linux.c) — bureau à distance, Linux uniquement (voir le commentaire
# en tête de capture_linux.c : pas de support Wayland dans ce premier passage).
if(TARGET_OS STREQUAL "linux" OR CMAKE_SYSTEM_NAME STREQUAL "Linux")
    find_package(X11 REQUIRED)
    if(NOT X11_Xext_FOUND OR NOT X11_Xtst_FOUND)
        message(FATAL_ERROR "libXext-dev/libxtst-dev introuvables — requis pour le bureau à distance "
                             "(voir platform/linux/capture_linux.c et input_linux.c)")
    endif()
    target_link_libraries(leo-agent PRIVATE X11::X11 X11::Xext X11::Xtst)
endif()

# ─── Informations de version compilées dans le binaire ───────────────────────
target_compile_definitions(leo-agent PRIVATE
    LEO_BUILD_VERSION="${CMAKE_PROJECT_VERSION}"
)

# ─── Installation ─────────────────────────────────────────────────────────────
# /opt/leo-one/agent n'a pas de sens sous Windows (pas de packaging/installeur
# Windows pour l'instant — hors périmètre, voir CMakeLists.txt du backend/
# frontend pour le reste du projet) : DESTINATION par défaut CMake (bin/) dans
# ce cas plutôt qu'un chemin Unix appliqué aveuglément.
if(WIN32)
    install(TARGETS leo-agent RUNTIME DESTINATION bin)
else()
    install(TARGETS leo-agent RUNTIME DESTINATION /opt/leo-one/agent)
endif()

# ─── Strip du binaire en Release (réduit la surface d'analyse) ───────────────
if(CMAKE_BUILD_TYPE STREQUAL "Release")
    add_custom_command(TARGET leo-agent POST_BUILD
        COMMAND ${CMAKE_STRIP} --strip-all $<TARGET_FILE:leo-agent>
        COMMENT "Strip du binaire de release"
    )
endif()
