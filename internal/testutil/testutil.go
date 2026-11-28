// Package testutil genera fixtures para los tests: ejecutables ELF estáticos
// mínimos y archivos tar.gz y zip deterministas. No lo usa el binario.
package testutil

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"
)

// TinyELF devuelve un ejecutable ELF64 estático para amd64 o arm64 que
// escribe msg en stdout y termina con código 0, sin importar sus argumentos.
// Son unos 160 bytes: ELF header, un PT_LOAD y el código.
func TinyELF(arch, msg string) []byte {
	const hdr = 64 + 56 // ELF header + un program header
	const base = 0x400000
	var code []byte
	var machine uint16
	n := uint32(len(msg))
	switch arch {
	case "amd64":
		machine = 62 // EM_X86_64
		code = []byte{
			0xb8, 1, 0, 0, 0, // mov eax, 1 (write)
			0xbf, 1, 0, 0, 0, // mov edi, 1
			0x48, 0x8d, 0x35, 16, 0, 0, 0, // lea rsi, [rip+16] -> msg
			0xba, 0, 0, 0, 0, // mov edx, n
			0x0f, 0x05, // syscall
			0xb8, 60, 0, 0, 0, // mov eax, 60 (exit)
			0x31, 0xff, // xor edi, edi
			0x0f, 0x05, // syscall
		}
		binary.LittleEndian.PutUint32(code[18:], n)
	case "arm64":
		machine = 183         // EM_AARCH64
		const msgOff = 32 - 4 // desde la instrucción adr (offset 4) hasta msg (offset 32)
		adr := uint32(0x10000000) | (msgOff&3)<<29 | (msgOff>>2)<<5 | 1
		for _, ins := range []uint32{
			0xd2800020,                 // mov x0, #1
			adr,                        // adr x1, msg
			0xd2800002 | (n&0xffff)<<5, // mov x2, #n
			0xd2800808,                 // mov x8, #64 (write)
			0xd4000001,                 // svc #0
			0xd2800000,                 // mov x0, #0
			0xd2800ba8,                 // mov x8, #93 (exit)
			0xd4000001,                 // svc #0
		} {
			code = binary.LittleEndian.AppendUint32(code, ins)
		}
	default:
		panic("arquitectura no soportada: " + arch)
	}
	body := append(code, msg...)
	total := uint64(hdr + len(body))

	var b bytes.Buffer
	le := binary.LittleEndian
	b.Write([]byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	binary.Write(&b, le, uint16(2))        // e_type: EXEC
	binary.Write(&b, le, machine)          // e_machine
	binary.Write(&b, le, uint32(1))        // e_version
	binary.Write(&b, le, uint64(base+hdr)) // e_entry
	binary.Write(&b, le, uint64(64))       // e_phoff
	binary.Write(&b, le, uint64(0))        // e_shoff
	binary.Write(&b, le, uint32(0))        // e_flags
	binary.Write(&b, le, uint16(64))       // e_ehsize
	binary.Write(&b, le, uint16(56))       // e_phentsize
	binary.Write(&b, le, uint16(1))        // e_phnum
	binary.Write(&b, le, uint16(64))       // e_shentsize
	binary.Write(&b, le, uint16(0))        // e_shnum
	binary.Write(&b, le, uint16(0))        // e_shstrndx
	binary.Write(&b, le, uint32(1))        // p_type: PT_LOAD
	binary.Write(&b, le, uint32(5))        // p_flags: R+X
	binary.Write(&b, le, uint64(0))        // p_offset
	binary.Write(&b, le, uint64(base))     // p_vaddr
	binary.Write(&b, le, uint64(base))     // p_paddr
	binary.Write(&b, le, total)            // p_filesz
	binary.Write(&b, le, total)            // p_memsz
	binary.Write(&b, le, uint64(0x1000))   // p_align
	b.Write(body)
	return b.Bytes()
}

// Entry es un miembro de un tar.gz o zip de prueba.
type Entry struct {
	Name string
	Mode int64  // 0 = 0644 (archivos) o 0755 (directorios)
	Body []byte // contenido de un archivo
	Dir  bool
	Link string // destino si es un enlace simbólico
}

// MTime es la fecha fija de todos los miembros.
var MTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// TarGz crea un tar.gz determinista.
func TarGz(entries []Entry) []byte {
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.Name, Mode: e.Mode, ModTime: MTime, Uname: "root", Gname: "root", Format: tar.FormatPAX}
		switch {
		case e.Dir:
			h.Typeflag = tar.TypeDir
			if h.Mode == 0 {
				h.Mode = 0o755
			}
		case e.Link != "":
			h.Typeflag, h.Linkname, h.Mode = tar.TypeSymlink, e.Link, 0o777
		default:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(e.Body))
			if h.Mode == 0 {
				h.Mode = 0o644
			}
		}
		tw.WriteHeader(h)
		tw.Write(e.Body)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// Zip crea un zip determinista.
func Zip(entries []Entry) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		fh := &zip.FileHeader{Name: e.Name, Method: zip.Deflate, Modified: MTime}
		mode := e.Mode
		switch {
		case e.Dir:
			fh.Name += "/"
			fh.SetMode(os.ModeDir | 0o755)
		case e.Link != "":
			fh.SetMode(os.ModeSymlink | 0o777)
		default:
			if mode == 0 {
				mode = 0o644
			}
			fh.SetMode(os.FileMode(mode))
		}
		w, _ := zw.CreateHeader(fh)
		if e.Link != "" {
			w.Write([]byte(e.Link))
		} else {
			w.Write(e.Body)
		}
	}
	zw.Close()
	return buf.Bytes()
}

// Redirect es un http.RoundTripper que envía al servidor de prueba las
// peticiones a cualquier host (github.com, raw.githubusercontent.com…) sin
// tocar la URL original, que es la que ve CheckRedirect.
type Redirect struct {
	Server *httptest.Server
}

// RoundTrip implementa http.RoundTripper.
func (r Redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = "https"
	r2.URL.Host = strings.TrimPrefix(r.Server.URL, "https://")
	r2.Host = req.URL.Host
	return r.Server.Client().Transport.RoundTrip(r2)
}

// MITText es un texto MIT estándar para fixtures.
const MITText = `MIT License

Copyright (c) 2026 Example Authors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

// ZlibText es un texto zlib estándar: una licencia libre que licensecheck
// identifica pero que no está en la allowlist de prueba (fase 005, cambio de
// licencia a una no permitida).
const ZlibText = `zlib License

Copyright (c) 2026 Example Authors

This software is provided 'as-is', without any express or implied
warranty. In no event will the authors be held liable for any damages
arising from the use of this software.

Permission is granted to anyone to use this software for any purpose,
including commercial applications, and to alter it and redistribute it
freely, subject to the following restrictions:

1. The origin of this software must not be misrepresented; you must not
   claim that you wrote the original software. If you use this software
   in a product, an acknowledgment in the product documentation would be
   appreciated but is not required.
2. Altered source versions must be plainly marked as such, and must not be
   misrepresented as being the original software.
3. This notice may not be removed or altered from any source distribution.
`

// WTFPLText es un texto WTFPL: licencia que licensecheck identifica y que no
// está en la allowlist de prueba (Zlib sí lo está desde la constitución 1.12.0).
const WTFPLText = `            DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
                    Version 2, December 2004

 Copyright (C) 2004 Sam Hocevar <sam@hocevar.net>

 Everyone is permitted to copy and distribute verbatim or modified
 copies of this license document, and changing it is allowed as long
 as the name is changed.

            DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
   TERMS AND CONDITIONS FOR COPYING, DISTRIBUTION AND MODIFICATION

  0. You just DO WHAT THE FUCK YOU WANT TO.
`
