import java.util.ArrayList;
import java.util.List;
import javax.xml.xpath.XPath;
import javax.xml.xpath.XPathFactory;
import org.w3c.dom.Document;

class Gt456Store {
    private final List<String> filters = new ArrayList<>();

    void save(String filter) {
        filters.add(filter);
    }

    String run(Document doc) throws Exception {
        XPath xpath = XPathFactory.newInstance().newXPath();
        return xpath.evaluate(filters.get(0), doc); // sink
    }
}
