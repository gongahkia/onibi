import javax.naming.directory.DirContext;
import javax.naming.directory.SearchControls;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/ldap")
class Gt426Servlet extends HttpServlet {
    private DirContext ctx;

    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        try {
            String user = req.getParameter("user");
            ctx.search("ou=people,dc=example,dc=com", "(uid=" + user + ")", new SearchControls()); // sink
        } catch (Exception ignored) {
        }
    }
}
