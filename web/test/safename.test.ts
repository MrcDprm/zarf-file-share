import { test } from "node:test";
import assert from "node:assert/strict";
import { safeName } from "../src/lib/safename.ts";

// The same cases as the Go package's tests, so both sides save under the same name.
const rlo = String.fromCharCode(0x202e);
const zwsp = String.fromCharCode(0x200b);

test("cleans names the way the Go package does", () => {
  const cases: [string, string][] = [
    ["rapor.pdf", "rapor.pdf"],
    ["Müşteri Sözleşmesi (son).docx", "Müşteri Sözleşmesi (son).docx"],
    ["../../etc/passwd", "passwd"],
    ["..\\..\\Windows\\win.ini", "win.ini"],
    ["/abs/path/file.txt", "file.txt"],
    ["..", "zarf-dosya"],
    [".bashrc", "bashrc"],
    ["", "zarf-dosya"],
    ["   ", "zarf-dosya"],
    ["CON.txt", "_CON.txt"],
    ["lpt1", "_lpt1"],
    ['a<b>c:d"e|f?g*h.txt', "a_b_c_d_e_f_g_h.txt"],
    [`fatura${rlo}txt.exe`, "faturatxt.exe"],
    ["satir\nsonu\t.txt", "satirsonu.txt"],
    ["sonunda nokta. . .", "sonunda nokta"],
    [`gizli${zwsp}karakter.png`, "gizlikarakter.png"],
  ];
  for (const [input, want] of cases) assert.equal(safeName(input), want, JSON.stringify(input));
});

test("long names keep their extension and stay under 200 bytes", () => {
  const got = safeName("ş".repeat(300) + ".pdf");
  assert.ok(new TextEncoder().encode(got).length <= 200);
  assert.ok(got.endsWith(".pdf") && got.startsWith("ş"));
});
