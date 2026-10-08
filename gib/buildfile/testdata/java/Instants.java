// Records how Jib's Instants.fromMillisOrIso8601 reads a time.
//
//   java -cp "$JIB_HOME/lib/*" Instants.java ../instants.json
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.File;
import java.time.Instant;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Random;
import java.util.Set;

public class Instants {
  static Map<String, Object> read(String text) {
    Map<String, Object> m = new LinkedHashMap<>();
    m.put("in", text);
    try {
      Instant i = com.google.cloud.tools.jib.cli.Instants.fromMillisOrIso8601(text, "t");
      m.put("seconds", i.getEpochSecond());
      m.put("nanos", i.getNano());
    } catch (IllegalArgumentException e) {
      m.put("refused", true);
    }
    return m;
  }

  static String[] a(String... s) {
    return s;
  }

  static String pick(Random r, String[] valid, String[] invalid) {
    String[] s = r.nextInt(10) < 9 ? valid : invalid;
    return s[r.nextInt(s.length)];
  }

  public static void main(String[] args) throws Exception {
    Set<String> in = new LinkedHashSet<>();
    String[] fixed = {
      "0", "1", "-1", "+5", "007", "1600000000000", "9223372036854775807", "-9223372036854775808",
      "9223372036854775808", "0x10", "1_000", " 5", "5 ", "", "-", "+", "١٢", "１",
      "2020-01-01T00:00:00Z", "2020-01-01T00:00Z", "2020-01-01t00:00:00z", "2020-01-01T00:00:00",
      "2020-01-01", "2020-01-01T00:00:00.123456789Z", "2020-01-01T00:00:00.1Z",
      "2020-01-01T00:00:00+01:00[Europe/Paris]", "2020-01-01T00:00:00[Europe/Paris]",
      "2020-10-25T02:30:00+05:00[Europe/Paris]", "2020-01-01T00:00:00Z[Zulu]",
      "+999999999-12-31T23:59:59.999999999-18:00", "-999999999-01-01T00:00+18:00",
      "+999999999-12-31T24:00Z", "2020-02-30T00:00Z", "2021-02-29T00:00Z", "2020-04-31T00:00Z",
      "2020-01-01T24:00Z", "2020-01-01T24:00:00.000Z", "2020-01-01T24:00:00.001Z",
      "2020-01-01T00:00:00Z[GMT0]", "2020-01-01T00:00:00Z[GMT+0]", "2020-01-01T00:00:00Z[UTC0]",
      "2020-01-01T00:00:00Z[UT]", "2020-01-01T00:00:00Z[UT+01:00]", "2020-01-01T00:00:00Z[UTC+01]",
      "2020-01-01T00:00:00Z[Etc/GMT+1]", "2020-01-01T00:00:00Z[SystemV/EST5]",
      "2020-01-01T00:00:00Z[+18:00]", "2020-01-01T00:00:00Z[+18:01]", "2020-01-01T00:00:00Z[+24:00]",
      "2020-01-01T00:00:00Z[-00:00:01]", "2020-01-01T00:00:00Z[GMTZ]", "2020-01-01T00:00:00Z[UTCZ]",
    };
    for (String f : fixed) {
      in.add(f);
    }
    Random r = new Random(7);
    while (in.size() < 3000) {
      String s =
          pick(r, a("2020", "1970", "1969", "-0001", "+12020", "+02020", "-2020", "+999999999", "-999999999"),
                  a("0000", "20200", "+2020", "999", "-0000", "+1000000000"))
              + "-" + pick(r, a("01", "12", "02", "04"), a("13", "00", "1"))
              + "-" + pick(r, a("01", "28", "29", "30", "31"), a("32", "00", "1"))
              + pick(r, a("T", "t"), a(" ", ""))
              + pick(r, a("00", "23", "12", "24"), a("25", "1"))
              + ":" + pick(r, a("00", "59"), a("60", "5"))
              + pick(r, a("", ":00", ":59", ":30"), a(":60", ":0", ":"))
              + pick(r, a("", ".", ".1", ".000", ".123456789", ".5"), a(".1234567891", ",5"))
              + pick(r, a("Z", "z", "+01:00", "+0100", "+01", "-01:30", "+01:00:30", "+18:00", "-0000",
                  "+0000", "-00", "+00:00", "+0130"), a("", "+18:01", "+24:00", "+60:00", "+1", "+01:0",
                  "+19", "+01:00:3", "+01:00:", "+01:60"))
              + pick(r, a("", "", "", "", "", "", "[UTC]", "[Europe/Paris]", "[Z]", "[GMT0]", "[UTC+01:00]", "[UT]",
                  "[+02:00]", "[Etc/GMT+1]", "[GMT]", "[UTC-18:00]", "[GMT+01:00:30]"),
                  a("[europe/paris]", "[GMT+0]", "[EST]", "[UT+01]", "[UTC", "[]"))
              + pick(r, a("", "", "", "", "", "", "", "", "+0100", "+0000", "-0100", "+01"), a("+01:00", "Z", " "));
      in.add(s);
    }
    List<Map<String, Object>> cases = new ArrayList<>();
    for (String s : in) {
      cases.add(read(s));
    }
    new ObjectMapper().writerWithDefaultPrettyPrinter().writeValue(new File(args[0]), cases);
  }
}
