// Automatically generated by go generate. DO NOT EDIT

package stdlib

var stdlibPackages = []string{"archive", "archive/tar", "archive/zip", "bufio", "bytes", "cmd", "cmd/addr2line", "cmd/api", "cmd/asm", "cmd/asm/internal", "cmd/asm/internal/arch", "cmd/asm/internal/asm", "cmd/asm/internal/flags", "cmd/asm/internal/lex", "cmd/buildid", "cmd/cgo", "cmd/compile", "cmd/compile/internal", "cmd/compile/internal/amd64", "cmd/compile/internal/arm", "cmd/compile/internal/arm64", "cmd/compile/internal/gc", "cmd/compile/internal/logopt", "cmd/compile/internal/mips", "cmd/compile/internal/mips64", "cmd/compile/internal/ppc64", "cmd/compile/internal/riscv64", "cmd/compile/internal/s390x", "cmd/compile/internal/ssa", "cmd/compile/internal/syntax", "cmd/compile/internal/test", "cmd/compile/internal/types", "cmd/compile/internal/wasm", "cmd/compile/internal/x86", "cmd/cover", "cmd/dist", "cmd/doc", "cmd/fix", "cmd/go", "cmd/go/internal", "cmd/go/internal/auth", "cmd/go/internal/base", "cmd/go/internal/bug", "cmd/go/internal/cache", "cmd/go/internal/cfg", "cmd/go/internal/clean", "cmd/go/internal/cmdflag", "cmd/go/internal/doc", "cmd/go/internal/envcmd", "cmd/go/internal/fix", "cmd/go/internal/fmtcmd", "cmd/go/internal/fsys", "cmd/go/internal/generate", "cmd/go/internal/get", "cmd/go/internal/help", "cmd/go/internal/imports", "cmd/go/internal/list", "cmd/go/internal/load", "cmd/go/internal/lockedfile", "cmd/go/internal/lockedfile/internal", "cmd/go/internal/lockedfile/internal/filelock", "cmd/go/internal/modcmd", "cmd/go/internal/modconv", "cmd/go/internal/modfetch", "cmd/go/internal/modfetch/codehost", "cmd/go/internal/modget", "cmd/go/internal/modinfo", "cmd/go/internal/modload", "cmd/go/internal/mvs", "cmd/go/internal/par", "cmd/go/internal/renameio", "cmd/go/internal/robustio", "cmd/go/internal/run", "cmd/go/internal/search", "cmd/go/internal/str", "cmd/go/internal/test", "cmd/go/internal/tool", "cmd/go/internal/trace", "cmd/go/internal/txtar", "cmd/go/internal/vcs", "cmd/go/internal/version", "cmd/go/internal/vet", "cmd/go/internal/web", "cmd/go/internal/work", "cmd/gofmt", "cmd/internal", "cmd/internal/archive", "cmd/internal/bio", "cmd/internal/browser", "cmd/internal/buildid", "cmd/internal/codesign", "cmd/internal/diff", "cmd/internal/dwarf", "cmd/internal/edit", "cmd/internal/gcprog", "cmd/internal/goobj", "cmd/internal/obj", "cmd/internal/obj/arm", "cmd/internal/obj/arm64", "cmd/internal/obj/mips", "cmd/internal/obj/ppc64", "cmd/internal/obj/riscv", "cmd/internal/obj/s390x", "cmd/internal/obj/wasm", "cmd/internal/obj/x86", "cmd/internal/objabi", "cmd/internal/objfile", "cmd/internal/pkgpath", "cmd/internal/src", "cmd/internal/sys", "cmd/internal/test2json", "cmd/internal/traceviewer", "cmd/link", "cmd/link/internal", "cmd/link/internal/amd64", "cmd/link/internal/arm", "cmd/link/internal/arm64", "cmd/link/internal/benchmark", "cmd/link/internal/ld", "cmd/link/internal/loadelf", "cmd/link/internal/loader", "cmd/link/internal/loadmacho", "cmd/link/internal/loadpe", "cmd/link/internal/loadxcoff", "cmd/link/internal/mips", "cmd/link/internal/mips64", "cmd/link/internal/ppc64", "cmd/link/internal/riscv64", "cmd/link/internal/s390x", "cmd/link/internal/sym", "cmd/link/internal/wasm", "cmd/link/internal/x86", "cmd/nm", "cmd/objdump", "cmd/pack", "cmd/pprof", "cmd/test2json", "cmd/trace", "cmd/vendor", "cmd/vendor/github.com", "cmd/vendor/github.com/google", "cmd/vendor/github.com/google/pprof", "cmd/vendor/github.com/google/pprof/driver", "cmd/vendor/github.com/google/pprof/internal", "cmd/vendor/github.com/google/pprof/internal/binutils", "cmd/vendor/github.com/google/pprof/internal/driver", "cmd/vendor/github.com/google/pprof/internal/elfexec", "cmd/vendor/github.com/google/pprof/internal/graph", "cmd/vendor/github.com/google/pprof/internal/measurement", "cmd/vendor/github.com/google/pprof/internal/plugin", "cmd/vendor/github.com/google/pprof/internal/report", "cmd/vendor/github.com/google/pprof/internal/symbolizer", "cmd/vendor/github.com/google/pprof/internal/symbolz", "cmd/vendor/github.com/google/pprof/internal/transport", "cmd/vendor/github.com/google/pprof/profile", "cmd/vendor/github.com/google/pprof/third_party", "cmd/vendor/github.com/google/pprof/third_party/d3", "cmd/vendor/github.com/google/pprof/third_party/d3flamegraph", "cmd/vendor/github.com/google/pprof/third_party/svgpan", "cmd/vendor/github.com/ianlancetaylor", "cmd/vendor/github.com/ianlancetaylor/demangle", "cmd/vendor/golang.org", "cmd/vendor/golang.org/x", "cmd/vendor/golang.org/x/arch", "cmd/vendor/golang.org/x/arch/arm", "cmd/vendor/golang.org/x/arch/arm/armasm", "cmd/vendor/golang.org/x/arch/arm64", "cmd/vendor/golang.org/x/arch/arm64/arm64asm", "cmd/vendor/golang.org/x/arch/ppc64", "cmd/vendor/golang.org/x/arch/ppc64/ppc64asm", "cmd/vendor/golang.org/x/arch/x86", "cmd/vendor/golang.org/x/arch/x86/x86asm", "cmd/vendor/golang.org/x/crypto", "cmd/vendor/golang.org/x/crypto/ed25519", "cmd/vendor/golang.org/x/crypto/ed25519/internal", "cmd/vendor/golang.org/x/crypto/ed25519/internal/edwards25519", "cmd/vendor/golang.org/x/crypto/ssh", "cmd/vendor/golang.org/x/crypto/ssh/terminal", "cmd/vendor/golang.org/x/mod", "cmd/vendor/golang.org/x/mod/internal", "cmd/vendor/golang.org/x/mod/internal/lazyregexp", "cmd/vendor/golang.org/x/mod/modfile", "cmd/vendor/golang.org/x/mod/module", "cmd/vendor/golang.org/x/mod/semver", "cmd/vendor/golang.org/x/mod/sumdb", "cmd/vendor/golang.org/x/mod/sumdb/dirhash", "cmd/vendor/golang.org/x/mod/sumdb/note", "cmd/vendor/golang.org/x/mod/sumdb/tlog", "cmd/vendor/golang.org/x/mod/zip", "cmd/vendor/golang.org/x/sys", "cmd/vendor/golang.org/x/sys/internal", "cmd/vendor/golang.org/x/sys/internal/unsafeheader", "cmd/vendor/golang.org/x/sys/unix", "cmd/vendor/golang.org/x/tools", "cmd/vendor/golang.org/x/tools/go", "cmd/vendor/golang.org/x/tools/go/analysis", "cmd/vendor/golang.org/x/tools/go/analysis/internal", "cmd/vendor/golang.org/x/tools/go/analysis/internal/analysisflags", "cmd/vendor/golang.org/x/tools/go/analysis/internal/facts", "cmd/vendor/golang.org/x/tools/go/analysis/passes", "cmd/vendor/golang.org/x/tools/go/analysis/passes/asmdecl", "cmd/vendor/golang.org/x/tools/go/analysis/passes/assign", "cmd/vendor/golang.org/x/tools/go/analysis/passes/atomic", "cmd/vendor/golang.org/x/tools/go/analysis/passes/bools", "cmd/vendor/golang.org/x/tools/go/analysis/passes/buildtag", "cmd/vendor/golang.org/x/tools/go/analysis/passes/cgocall", "cmd/vendor/golang.org/x/tools/go/analysis/passes/composite", "cmd/vendor/golang.org/x/tools/go/analysis/passes/copylock", "cmd/vendor/golang.org/x/tools/go/analysis/passes/ctrlflow", "cmd/vendor/golang.org/x/tools/go/analysis/passes/errorsas", "cmd/vendor/golang.org/x/tools/go/analysis/passes/framepointer", "cmd/vendor/golang.org/x/tools/go/analysis/passes/httpresponse", "cmd/vendor/golang.org/x/tools/go/analysis/passes/ifaceassert", "cmd/vendor/golang.org/x/tools/go/analysis/passes/inspect", "cmd/vendor/golang.org/x/tools/go/analysis/passes/internal", "cmd/vendor/golang.org/x/tools/go/analysis/passes/internal/analysisutil", "cmd/vendor/golang.org/x/tools/go/analysis/passes/loopclosure", "cmd/vendor/golang.org/x/tools/go/analysis/passes/lostcancel", "cmd/vendor/golang.org/x/tools/go/analysis/passes/nilfunc", "cmd/vendor/golang.org/x/tools/go/analysis/passes/printf", "cmd/vendor/golang.org/x/tools/go/analysis/passes/shift", "cmd/vendor/golang.org/x/tools/go/analysis/passes/stdmethods", "cmd/vendor/golang.org/x/tools/go/analysis/passes/stringintconv", "cmd/vendor/golang.org/x/tools/go/analysis/passes/structtag", "cmd/vendor/golang.org/x/tools/go/analysis/passes/testinggoroutine", "cmd/vendor/golang.org/x/tools/go/analysis/passes/tests", "cmd/vendor/golang.org/x/tools/go/analysis/passes/unmarshal", "cmd/vendor/golang.org/x/tools/go/analysis/passes/unreachable", "cmd/vendor/golang.org/x/tools/go/analysis/passes/unsafeptr", "cmd/vendor/golang.org/x/tools/go/analysis/passes/unusedresult", "cmd/vendor/golang.org/x/tools/go/analysis/unitchecker", "cmd/vendor/golang.org/x/tools/go/ast", "cmd/vendor/golang.org/x/tools/go/ast/astutil", "cmd/vendor/golang.org/x/tools/go/ast/inspector", "cmd/vendor/golang.org/x/tools/go/cfg", "cmd/vendor/golang.org/x/tools/go/types", "cmd/vendor/golang.org/x/tools/go/types/objectpath", "cmd/vendor/golang.org/x/tools/go/types/typeutil", "cmd/vendor/golang.org/x/tools/internal", "cmd/vendor/golang.org/x/tools/internal/analysisinternal", "cmd/vendor/golang.org/x/tools/internal/lsp", "cmd/vendor/golang.org/x/tools/internal/lsp/fuzzy", "cmd/vendor/golang.org/x/xerrors", "cmd/vendor/golang.org/x/xerrors/internal", "cmd/vet", "compress", "compress/bzip2", "compress/flate", "compress/gzip", "compress/lzw", "compress/zlib", "container", "container/heap", "container/list", "container/ring", "context", "crypto", "crypto/aes", "crypto/cipher", "crypto/des", "crypto/dsa", "crypto/ecdsa", "crypto/ed25519", "crypto/ed25519/internal", "crypto/ed25519/internal/edwards25519", "crypto/elliptic", "crypto/hmac", "crypto/internal", "crypto/internal/randutil", "crypto/internal/subtle", "crypto/md5", "crypto/rand", "crypto/rc4", "crypto/rsa", "crypto/sha1", "crypto/sha256", "crypto/sha512", "crypto/subtle", "crypto/tls", "crypto/x509", "crypto/x509/pkix", "database", "database/sql", "database/sql/driver", "debug", "debug/dwarf", "debug/elf", "debug/gosym", "debug/macho", "debug/pe", "debug/plan9obj", "embed", "encoding", "encoding/ascii85", "encoding/asn1", "encoding/base32", "encoding/base64", "encoding/binary", "encoding/csv", "encoding/gob", "encoding/hex", "encoding/json", "encoding/pem", "encoding/xml", "errors", "expvar", "flag", "fmt", "go", "go/ast", "go/build", "go/build/constraint", "go/constant", "go/doc", "go/format", "go/importer", "go/internal", "go/internal/gccgoimporter", "go/internal/gcimporter", "go/internal/srcimporter", "go/parser", "go/printer", "go/scanner", "go/token", "go/types", "hash", "hash/adler32", "hash/crc32", "hash/crc64", "hash/fnv", "hash/maphash", "html", "html/template", "image", "image/color", "image/color/palette", "image/draw", "image/gif", "image/internal", "image/internal/imageutil", "image/jpeg", "image/png", "index", "index/suffixarray", "internal", "internal/bytealg", "internal/cfg", "internal/cpu", "internal/execabs", "internal/fmtsort", "internal/goroot", "internal/goversion", "internal/lazyregexp", "internal/lazytemplate", "internal/nettrace", "internal/obscuretestdata", "internal/oserror", "internal/poll", "internal/profile", "internal/race", "internal/reflectlite", "internal/singleflight", "internal/syscall", "internal/syscall/execenv", "internal/syscall/unix", "internal/sysinfo", "internal/testenv", "internal/testlog", "internal/trace", "internal/unsafeheader", "internal/xcoff", "io", "io/fs", "io/ioutil", "log", "log/syslog", "math", "math/big", "math/bits", "math/cmplx", "math/rand", "mime", "mime/multipart", "mime/quotedprintable", "net", "net/http", "net/http/cgi", "net/http/cookiejar", "net/http/fcgi", "net/http/httptest", "net/http/httptrace", "net/http/httputil", "net/http/internal", "net/http/pprof", "net/internal", "net/internal/socktest", "net/mail", "net/rpc", "net/rpc/jsonrpc", "net/smtp", "net/textproto", "net/url", "os", "os/exec", "os/signal", "os/signal/internal", "os/signal/internal/pty", "os/user", "path", "path/filepath", "plugin", "reflect", "regexp", "regexp/syntax", "runtime", "runtime/cgo", "runtime/debug", "runtime/internal", "runtime/internal/atomic", "runtime/internal/math", "runtime/internal/sys", "runtime/metrics", "runtime/pprof", "runtime/race", "runtime/trace", "sort", "strconv", "strings", "sync", "sync/atomic", "syscall", "testing", "testing/fstest", "testing/internal", "testing/internal/testdeps", "testing/iotest", "testing/quick", "text", "text/scanner", "text/tabwriter", "text/template", "text/template/parse", "time", "time/tzdata", "unicode", "unicode/utf16", "unicode/utf8", "unsafe"}

var stdlibPackagesMap = map[string]struct {}{"archive":struct {}{}, "archive/tar":struct {}{}, "archive/zip":struct {}{}, "bufio":struct {}{}, "bytes":struct {}{}, "cmd":struct {}{}, "cmd/addr2line":struct {}{}, "cmd/api":struct {}{}, "cmd/asm":struct {}{}, "cmd/asm/internal":struct {}{}, "cmd/asm/internal/arch":struct {}{}, "cmd/asm/internal/asm":struct {}{}, "cmd/asm/internal/flags":struct {}{}, "cmd/asm/internal/lex":struct {}{}, "cmd/buildid":struct {}{}, "cmd/cgo":struct {}{}, "cmd/compile":struct {}{}, "cmd/compile/internal":struct {}{}, "cmd/compile/internal/amd64":struct {}{}, "cmd/compile/internal/arm":struct {}{}, "cmd/compile/internal/arm64":struct {}{}, "cmd/compile/internal/gc":struct {}{}, "cmd/compile/internal/logopt":struct {}{}, "cmd/compile/internal/mips":struct {}{}, "cmd/compile/internal/mips64":struct {}{}, "cmd/compile/internal/ppc64":struct {}{}, "cmd/compile/internal/riscv64":struct {}{}, "cmd/compile/internal/s390x":struct {}{}, "cmd/compile/internal/ssa":struct {}{}, "cmd/compile/internal/syntax":struct {}{}, "cmd/compile/internal/test":struct {}{}, "cmd/compile/internal/types":struct {}{}, "cmd/compile/internal/wasm":struct {}{}, "cmd/compile/internal/x86":struct {}{}, "cmd/cover":struct {}{}, "cmd/dist":struct {}{}, "cmd/doc":struct {}{}, "cmd/fix":struct {}{}, "cmd/go":struct {}{}, "cmd/go/internal":struct {}{}, "cmd/go/internal/auth":struct {}{}, "cmd/go/internal/base":struct {}{}, "cmd/go/internal/bug":struct {}{}, "cmd/go/internal/cache":struct {}{}, "cmd/go/internal/cfg":struct {}{}, "cmd/go/internal/clean":struct {}{}, "cmd/go/internal/cmdflag":struct {}{}, "cmd/go/internal/doc":struct {}{}, "cmd/go/internal/envcmd":struct {}{}, "cmd/go/internal/fix":struct {}{}, "cmd/go/internal/fmtcmd":struct {}{}, "cmd/go/internal/fsys":struct {}{}, "cmd/go/internal/generate":struct {}{}, "cmd/go/internal/get":struct {}{}, "cmd/go/internal/help":struct {}{}, "cmd/go/internal/imports":struct {}{}, "cmd/go/internal/list":struct {}{}, "cmd/go/internal/load":struct {}{}, "cmd/go/internal/lockedfile":struct {}{}, "cmd/go/internal/lockedfile/internal":struct {}{}, "cmd/go/internal/lockedfile/internal/filelock":struct {}{}, "cmd/go/internal/modcmd":struct {}{}, "cmd/go/internal/modconv":struct {}{}, "cmd/go/internal/modfetch":struct {}{}, "cmd/go/internal/modfetch/codehost":struct {}{}, "cmd/go/internal/modget":struct {}{}, "cmd/go/internal/modinfo":struct {}{}, "cmd/go/internal/modload":struct {}{}, "cmd/go/internal/mvs":struct {}{}, "cmd/go/internal/par":struct {}{}, "cmd/go/internal/renameio":struct {}{}, "cmd/go/internal/robustio":struct {}{}, "cmd/go/internal/run":struct {}{}, "cmd/go/internal/search":struct {}{}, "cmd/go/internal/str":struct {}{}, "cmd/go/internal/test":struct {}{}, "cmd/go/internal/tool":struct {}{}, "cmd/go/internal/trace":struct {}{}, "cmd/go/internal/txtar":struct {}{}, "cmd/go/internal/vcs":struct {}{}, "cmd/go/internal/version":struct {}{}, "cmd/go/internal/vet":struct {}{}, "cmd/go/internal/web":struct {}{}, "cmd/go/internal/work":struct {}{}, "cmd/gofmt":struct {}{}, "cmd/internal":struct {}{}, "cmd/internal/archive":struct {}{}, "cmd/internal/bio":struct {}{}, "cmd/internal/browser":struct {}{}, "cmd/internal/buildid":struct {}{}, "cmd/internal/codesign":struct {}{}, "cmd/internal/diff":struct {}{}, "cmd/internal/dwarf":struct {}{}, "cmd/internal/edit":struct {}{}, "cmd/internal/gcprog":struct {}{}, "cmd/internal/goobj":struct {}{}, "cmd/internal/obj":struct {}{}, "cmd/internal/obj/arm":struct {}{}, "cmd/internal/obj/arm64":struct {}{}, "cmd/internal/obj/mips":struct {}{}, "cmd/internal/obj/ppc64":struct {}{}, "cmd/internal/obj/riscv":struct {}{}, "cmd/internal/obj/s390x":struct {}{}, "cmd/internal/obj/wasm":struct {}{}, "cmd/internal/obj/x86":struct {}{}, "cmd/internal/objabi":struct {}{}, "cmd/internal/objfile":struct {}{}, "cmd/internal/pkgpath":struct {}{}, "cmd/internal/src":struct {}{}, "cmd/internal/sys":struct {}{}, "cmd/internal/test2json":struct {}{}, "cmd/internal/traceviewer":struct {}{}, "cmd/link":struct {}{}, "cmd/link/internal":struct {}{}, "cmd/link/internal/amd64":struct {}{}, "cmd/link/internal/arm":struct {}{}, "cmd/link/internal/arm64":struct {}{}, "cmd/link/internal/benchmark":struct {}{}, "cmd/link/internal/ld":struct {}{}, "cmd/link/internal/loadelf":struct {}{}, "cmd/link/internal/loader":struct {}{}, "cmd/link/internal/loadmacho":struct {}{}, "cmd/link/internal/loadpe":struct {}{}, "cmd/link/internal/loadxcoff":struct {}{}, "cmd/link/internal/mips":struct {}{}, "cmd/link/internal/mips64":struct {}{}, "cmd/link/internal/ppc64":struct {}{}, "cmd/link/internal/riscv64":struct {}{}, "cmd/link/internal/s390x":struct {}{}, "cmd/link/internal/sym":struct {}{}, "cmd/link/internal/wasm":struct {}{}, "cmd/link/internal/x86":struct {}{}, "cmd/nm":struct {}{}, "cmd/objdump":struct {}{}, "cmd/pack":struct {}{}, "cmd/pprof":struct {}{}, "cmd/test2json":struct {}{}, "cmd/trace":struct {}{}, "cmd/vendor":struct {}{}, "cmd/vendor/github.com":struct {}{}, "cmd/vendor/github.com/google":struct {}{}, "cmd/vendor/github.com/google/pprof":struct {}{}, "cmd/vendor/github.com/google/pprof/driver":struct {}{}, "cmd/vendor/github.com/google/pprof/internal":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/binutils":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/driver":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/elfexec":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/graph":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/measurement":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/plugin":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/report":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/symbolizer":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/symbolz":struct {}{}, "cmd/vendor/github.com/google/pprof/internal/transport":struct {}{}, "cmd/vendor/github.com/google/pprof/profile":struct {}{}, "cmd/vendor/github.com/google/pprof/third_party":struct {}{}, "cmd/vendor/github.com/google/pprof/third_party/d3":struct {}{}, "cmd/vendor/github.com/google/pprof/third_party/d3flamegraph":struct {}{}, "cmd/vendor/github.com/google/pprof/third_party/svgpan":struct {}{}, "cmd/vendor/github.com/ianlancetaylor":struct {}{}, "cmd/vendor/github.com/ianlancetaylor/demangle":struct {}{}, "cmd/vendor/golang.org":struct {}{}, "cmd/vendor/golang.org/x":struct {}{}, "cmd/vendor/golang.org/x/arch":struct {}{}, "cmd/vendor/golang.org/x/arch/arm":struct {}{}, "cmd/vendor/golang.org/x/arch/arm/armasm":struct {}{}, "cmd/vendor/golang.org/x/arch/arm64":struct {}{}, "cmd/vendor/golang.org/x/arch/arm64/arm64asm":struct {}{}, "cmd/vendor/golang.org/x/arch/ppc64":struct {}{}, "cmd/vendor/golang.org/x/arch/ppc64/ppc64asm":struct {}{}, "cmd/vendor/golang.org/x/arch/x86":struct {}{}, "cmd/vendor/golang.org/x/arch/x86/x86asm":struct {}{}, "cmd/vendor/golang.org/x/crypto":struct {}{}, "cmd/vendor/golang.org/x/crypto/ed25519":struct {}{}, "cmd/vendor/golang.org/x/crypto/ed25519/internal":struct {}{}, "cmd/vendor/golang.org/x/crypto/ed25519/internal/edwards25519":struct {}{}, "cmd/vendor/golang.org/x/crypto/ssh":struct {}{}, "cmd/vendor/golang.org/x/crypto/ssh/terminal":struct {}{}, "cmd/vendor/golang.org/x/mod":struct {}{}, "cmd/vendor/golang.org/x/mod/internal":struct {}{}, "cmd/vendor/golang.org/x/mod/internal/lazyregexp":struct {}{}, "cmd/vendor/golang.org/x/mod/modfile":struct {}{}, "cmd/vendor/golang.org/x/mod/module":struct {}{}, "cmd/vendor/golang.org/x/mod/semver":struct {}{}, "cmd/vendor/golang.org/x/mod/sumdb":struct {}{}, "cmd/vendor/golang.org/x/mod/sumdb/dirhash":struct {}{}, "cmd/vendor/golang.org/x/mod/sumdb/note":struct {}{}, "cmd/vendor/golang.org/x/mod/sumdb/tlog":struct {}{}, "cmd/vendor/golang.org/x/mod/zip":struct {}{}, "cmd/vendor/golang.org/x/sys":struct {}{}, "cmd/vendor/golang.org/x/sys/internal":struct {}{}, "cmd/vendor/golang.org/x/sys/internal/unsafeheader":struct {}{}, "cmd/vendor/golang.org/x/sys/unix":struct {}{}, "cmd/vendor/golang.org/x/tools":struct {}{}, "cmd/vendor/golang.org/x/tools/go":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/internal":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/internal/analysisflags":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/internal/facts":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/asmdecl":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/assign":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/atomic":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/bools":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/buildtag":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/cgocall":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/composite":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/copylock":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/ctrlflow":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/errorsas":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/framepointer":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/httpresponse":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/ifaceassert":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/inspect":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/internal":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/internal/analysisutil":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/loopclosure":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/lostcancel":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/nilfunc":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/printf":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/shift":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/stdmethods":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/stringintconv":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/structtag":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/testinggoroutine":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/tests":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/unmarshal":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/unreachable":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/unsafeptr":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/passes/unusedresult":struct {}{}, "cmd/vendor/golang.org/x/tools/go/analysis/unitchecker":struct {}{}, "cmd/vendor/golang.org/x/tools/go/ast":struct {}{}, "cmd/vendor/golang.org/x/tools/go/ast/astutil":struct {}{}, "cmd/vendor/golang.org/x/tools/go/ast/inspector":struct {}{}, "cmd/vendor/golang.org/x/tools/go/cfg":struct {}{}, "cmd/vendor/golang.org/x/tools/go/types":struct {}{}, "cmd/vendor/golang.org/x/tools/go/types/objectpath":struct {}{}, "cmd/vendor/golang.org/x/tools/go/types/typeutil":struct {}{}, "cmd/vendor/golang.org/x/tools/internal":struct {}{}, "cmd/vendor/golang.org/x/tools/internal/analysisinternal":struct {}{}, "cmd/vendor/golang.org/x/tools/internal/lsp":struct {}{}, "cmd/vendor/golang.org/x/tools/internal/lsp/fuzzy":struct {}{}, "cmd/vendor/golang.org/x/xerrors":struct {}{}, "cmd/vendor/golang.org/x/xerrors/internal":struct {}{}, "cmd/vet":struct {}{}, "compress":struct {}{}, "compress/bzip2":struct {}{}, "compress/flate":struct {}{}, "compress/gzip":struct {}{}, "compress/lzw":struct {}{}, "compress/zlib":struct {}{}, "container":struct {}{}, "container/heap":struct {}{}, "container/list":struct {}{}, "container/ring":struct {}{}, "context":struct {}{}, "crypto":struct {}{}, "crypto/aes":struct {}{}, "crypto/cipher":struct {}{}, "crypto/des":struct {}{}, "crypto/dsa":struct {}{}, "crypto/ecdsa":struct {}{}, "crypto/ed25519":struct {}{}, "crypto/ed25519/internal":struct {}{}, "crypto/ed25519/internal/edwards25519":struct {}{}, "crypto/elliptic":struct {}{}, "crypto/hmac":struct {}{}, "crypto/internal":struct {}{}, "crypto/internal/randutil":struct {}{}, "crypto/internal/subtle":struct {}{}, "crypto/md5":struct {}{}, "crypto/rand":struct {}{}, "crypto/rc4":struct {}{}, "crypto/rsa":struct {}{}, "crypto/sha1":struct {}{}, "crypto/sha256":struct {}{}, "crypto/sha512":struct {}{}, "crypto/subtle":struct {}{}, "crypto/tls":struct {}{}, "crypto/x509":struct {}{}, "crypto/x509/pkix":struct {}{}, "database":struct {}{}, "database/sql":struct {}{}, "database/sql/driver":struct {}{}, "debug":struct {}{}, "debug/dwarf":struct {}{}, "debug/elf":struct {}{}, "debug/gosym":struct {}{}, "debug/macho":struct {}{}, "debug/pe":struct {}{}, "debug/plan9obj":struct {}{}, "embed":struct {}{}, "encoding":struct {}{}, "encoding/ascii85":struct {}{}, "encoding/asn1":struct {}{}, "encoding/base32":struct {}{}, "encoding/base64":struct {}{}, "encoding/binary":struct {}{}, "encoding/csv":struct {}{}, "encoding/gob":struct {}{}, "encoding/hex":struct {}{}, "encoding/json":struct {}{}, "encoding/pem":struct {}{}, "encoding/xml":struct {}{}, "errors":struct {}{}, "expvar":struct {}{}, "flag":struct {}{}, "fmt":struct {}{}, "go":struct {}{}, "go/ast":struct {}{}, "go/build":struct {}{}, "go/build/constraint":struct {}{}, "go/constant":struct {}{}, "go/doc":struct {}{}, "go/format":struct {}{}, "go/importer":struct {}{}, "go/internal":struct {}{}, "go/internal/gccgoimporter":struct {}{}, "go/internal/gcimporter":struct {}{}, "go/internal/srcimporter":struct {}{}, "go/parser":struct {}{}, "go/printer":struct {}{}, "go/scanner":struct {}{}, "go/token":struct {}{}, "go/types":struct {}{}, "hash":struct {}{}, "hash/adler32":struct {}{}, "hash/crc32":struct {}{}, "hash/crc64":struct {}{}, "hash/fnv":struct {}{}, "hash/maphash":struct {}{}, "html":struct {}{}, "html/template":struct {}{}, "image":struct {}{}, "image/color":struct {}{}, "image/color/palette":struct {}{}, "image/draw":struct {}{}, "image/gif":struct {}{}, "image/internal":struct {}{}, "image/internal/imageutil":struct {}{}, "image/jpeg":struct {}{}, "image/png":struct {}{}, "index":struct {}{}, "index/suffixarray":struct {}{}, "internal":struct {}{}, "internal/bytealg":struct {}{}, "internal/cfg":struct {}{}, "internal/cpu":struct {}{}, "internal/execabs":struct {}{}, "internal/fmtsort":struct {}{}, "internal/goroot":struct {}{}, "internal/goversion":struct {}{}, "internal/lazyregexp":struct {}{}, "internal/lazytemplate":struct {}{}, "internal/nettrace":struct {}{}, "internal/obscuretestdata":struct {}{}, "internal/oserror":struct {}{}, "internal/poll":struct {}{}, "internal/profile":struct {}{}, "internal/race":struct {}{}, "internal/reflectlite":struct {}{}, "internal/singleflight":struct {}{}, "internal/syscall":struct {}{}, "internal/syscall/execenv":struct {}{}, "internal/syscall/unix":struct {}{}, "internal/sysinfo":struct {}{}, "internal/testenv":struct {}{}, "internal/testlog":struct {}{}, "internal/trace":struct {}{}, "internal/unsafeheader":struct {}{}, "internal/xcoff":struct {}{}, "io":struct {}{}, "io/fs":struct {}{}, "io/ioutil":struct {}{}, "log":struct {}{}, "log/syslog":struct {}{}, "math":struct {}{}, "math/big":struct {}{}, "math/bits":struct {}{}, "math/cmplx":struct {}{}, "math/rand":struct {}{}, "mime":struct {}{}, "mime/multipart":struct {}{}, "mime/quotedprintable":struct {}{}, "net":struct {}{}, "net/http":struct {}{}, "net/http/cgi":struct {}{}, "net/http/cookiejar":struct {}{}, "net/http/fcgi":struct {}{}, "net/http/httptest":struct {}{}, "net/http/httptrace":struct {}{}, "net/http/httputil":struct {}{}, "net/http/internal":struct {}{}, "net/http/pprof":struct {}{}, "net/internal":struct {}{}, "net/internal/socktest":struct {}{}, "net/mail":struct {}{}, "net/rpc":struct {}{}, "net/rpc/jsonrpc":struct {}{}, "net/smtp":struct {}{}, "net/textproto":struct {}{}, "net/url":struct {}{}, "os":struct {}{}, "os/exec":struct {}{}, "os/signal":struct {}{}, "os/signal/internal":struct {}{}, "os/signal/internal/pty":struct {}{}, "os/user":struct {}{}, "path":struct {}{}, "path/filepath":struct {}{}, "plugin":struct {}{}, "reflect":struct {}{}, "regexp":struct {}{}, "regexp/syntax":struct {}{}, "runtime":struct {}{}, "runtime/cgo":struct {}{}, "runtime/debug":struct {}{}, "runtime/internal":struct {}{}, "runtime/internal/atomic":struct {}{}, "runtime/internal/math":struct {}{}, "runtime/internal/sys":struct {}{}, "runtime/metrics":struct {}{}, "runtime/pprof":struct {}{}, "runtime/race":struct {}{}, "runtime/trace":struct {}{}, "sort":struct {}{}, "strconv":struct {}{}, "strings":struct {}{}, "sync":struct {}{}, "sync/atomic":struct {}{}, "syscall":struct {}{}, "testing":struct {}{}, "testing/fstest":struct {}{}, "testing/internal":struct {}{}, "testing/internal/testdeps":struct {}{}, "testing/iotest":struct {}{}, "testing/quick":struct {}{}, "text":struct {}{}, "text/scanner":struct {}{}, "text/tabwriter":struct {}{}, "text/template":struct {}{}, "text/template/parse":struct {}{}, "time":struct {}{}, "time/tzdata":struct {}{}, "unicode":struct {}{}, "unicode/utf16":struct {}{}, "unicode/utf8":struct {}{}, "unsafe":struct {}{}}
