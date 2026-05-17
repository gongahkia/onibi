import javax.xml.namespace.QName;
import javax.xml.xpath.*;

class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request, Object doc) throws Exception {
        XPathFactory factory = XPathFactory.newInstance();
        XPath xpath = factory.newXPath();
        xpath.setXPathVariableResolver(name -> {
            if ("username".equals(name.getLocalPart())) {
                return request.getParameter("username");
            }
            return null;
        });
        return xpath.evaluate("//user[name=$username]", doc);
    }
}
