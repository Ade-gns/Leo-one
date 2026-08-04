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
    src/logger.c
    src/protocol.c
    src/connection.c
    src/metrics.c
    third_party/cjson/cJSON.c
)

# ─── Sources spécifiques à la plateforme ─────────────────────────────────────
if(TARGET_OS STREQUAL "linux" OR CMAKE_SYSTEM_NAME STREQUAL "Linux")
    list(APPEND AGENT_COMMON_SOURCES
        platform/linux/metrics_linux.c
        platform/linux/service_linux.c
        platform/linux/executor_linux.c
        platform/linux/crypto_linux.c
    )
    add_definitions(-DLEO_PLATFORM_LINUX)

elseif(TARGET_OS STREQUAL "windows" OR CMAKE_SYSTEM_NAME STREQUAL "Windows")
    list(APPEND AGENT_COMMON_SOURCES
        platform/windows/metrics_win.c
        platform/windows/service_win.c
        platform/windows/executor_win.c
        platform/windows/crypto_win.c
    )
    add_definitions(-DLEO_PLATFORM_WINDOWS)

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
    -Wl,-z,relro -Wl,-z,now   # RELRO complet (Linux uniquement)
)

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
add_subdirectory(third_party/libwebsockets EXCLUDE_FROM_ALL)
# La cible statique s'appelle "websockets" dans libwebsockets >= 4.x
# ("websockets_static" est un nom d'une ancienne version, jamais buildé ici).
target_link_libraries(leo-agent PRIVATE websockets)

# Threads POSIX
find_package(Threads REQUIRED)
target_link_libraries(leo-agent PRIVATE Threads::Threads)

# OpenSSL (nécessaire pour libwebsockets)
find_package(OpenSSL REQUIRED)
target_link_libraries(leo-agent PRIVATE OpenSSL::SSL OpenSSL::Crypto)

# ─── Informations de version compilées dans le binaire ───────────────────────
target_compile_definitions(leo-agent PRIVATE
    LEO_BUILD_VERSION="${CMAKE_PROJECT_VERSION}"
)

# ─── Installation ─────────────────────────────────────────────────────────────
install(TARGETS leo-agent
    RUNTIME DESTINATION /opt/leo-one/agent
)

# ─── Strip du binaire en Release (réduit la surface d'analyse) ───────────────
if(CMAKE_BUILD_TYPE STREQUAL "Release")
    add_custom_command(TARGET leo-agent POST_BUILD
        COMMAND ${CMAKE_STRIP} --strip-all $<TARGET_FILE:leo-agent>
        COMMENT "Strip du binaire de release"
    )
endif()
