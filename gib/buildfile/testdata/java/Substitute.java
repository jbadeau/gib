// Records how Jib substitutes template parameters: commons-text's
// StringSubstitutorReader, as BuildFiles reads a build file through it.
//
//   java -cp "$JIB_HOME/lib/*" Substitute.java ../substitute.json
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.BufferedReader;
import java.io.File;
import java.io.StringReader;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.Set;
import org.apache.commons.text.StringSubstitutor;
import org.apache.commons.text.io.StringSubstitutorReader;

public class Substitute {
  static final Map<String, String> params = new LinkedHashMap<>();

  static Map<String, String> read(String text) {
    StringSubstitutor s = new StringSubstitutor(params).setEnableUndefinedVariableException(true);
    Map<String, String> m = new LinkedHashMap<>();
    m.put("in", text);
    try (StringSubstitutorReader r =
        new StringSubstitutorReader(new BufferedReader(new StringReader(text)), s)) {
      StringBuilder out = new StringBuilder();
      char[] buf = new char[1024];
      for (int n; (n = r.read(buf, 0, buf.length)) != -1; ) {
        out.append(buf, 0, n);
      }
      m.put("out", out.toString());
    } catch (Exception e) {
      m.put("err", e.getMessage());
    }
    return m;
  }

  public static void main(String[] args) throws Exception {
    String[][] p = {
      {"a", "A"}, {"b", "B"}, {"ref", "${a}"}, {"self", "${self}"}, {"c1", "${c2}"}, {"c2", "${c1}"},
      {"dol", "$"}, {"pre", "${"}, {"d", "${zz:-D}"}, {"esc", "$${a}"}, {"", "E"}, {"a:", "colon"},
    };
    for (String[] kv : p) {
      params.put(kv[0], kv[1]);
    }
    String[] fixed = {
      "${a}", "x${a}y${b}z", "$${a}", "$$${a}", "$$$${a}", "${zz}", "${zz:-def}", "${zz:-}",
      "${a:-def}", "${zz:-${a}}", "${zz:-${yy}}", "${ref}", "${self}", "${c1}", "${${b}}", "${a${b}}",
      "${a", "x ${a", "${", "$", "$$", "${}", "${:-x}", "${x}}", "${a}}", "${esc}", "${d}", "$a",
      "${ a}", "${a }", "${zz:=x}", "${zz:-x:-y}", "${{a}}", "{${a}}", "${$a}", "${\n}",
      "$${zz}", "$${${a}}", "${a ${b}", "$${a ${b}", "${a}$${b}", "$${a}${b}", "${dol}${a}",
      "${pre}a}", "${dol}{a}", "$${", "${a}$", "${zz:-${a}}${b}", "${zz:-$${a}}",
      "key: ${a}\nother: ${b}\n",
    };
    List<Map<String, String>> cases = new ArrayList<>();
    Set<String> seen = new HashSet<>();
    for (String t : fixed) {
      seen.add(t);
      cases.add(read(t));
    }
    String[] atoms = {
      "$", "$", "{", "}", "a", "b", ":-", "zz", "x", "\n", "ref", "dol", "pre", "d", "esc", "self", ":"
    };
    Random rnd = new Random(42);
    while (cases.size() < 1500) {
      StringBuilder sb = new StringBuilder();
      for (int i = 0, n = 1 + rnd.nextInt(14); i < n; i++) {
        sb.append(atoms[rnd.nextInt(atoms.length)]);
      }
      String t = sb.toString();
      if (t.contains("${") && seen.add(t)) {
        cases.add(read(t));
      }
    }
    Map<String, Object> doc = new LinkedHashMap<>();
    doc.put("params", params);
    doc.put("cases", cases);
    new ObjectMapper().writerWithDefaultPrettyPrinter().writeValue(new File(args[0]), doc);
  }
}
