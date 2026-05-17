import javax.xml.xpath.*;

class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request) throws Exception {
        XPathFactory factory = XPathFactory.newInstance();
        XPath xpath = factory.newXPath();
        String expr = "//user[name='" + request.getParameter("username") + "']";
        return xpath.compile(expr);
    }
}
