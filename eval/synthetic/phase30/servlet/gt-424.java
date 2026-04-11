import javax.xml.xpath.XPath;
import javax.xml.xpath.XPathFactory;
import org.w3c.dom.Document;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/directory")
class Gt424Servlet extends HttpServlet {
    private Document directory;

    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        try {
            String name = req.getParameter("name");
            XPath xpath = XPathFactory.newInstance().newXPath();
            String email = xpath.evaluate("//users/user[name/text()='" + name + "']/email/text()", directory); // sink
            resp.getWriter().write(email);
        } catch (Exception ignored) {
        }
    }
}
