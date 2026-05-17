import org.springframework.web.bind.annotation.*;
import org.springframework.data.mongodb.core.query.BasicQuery;

@RestController
class DemoController {
    String handler(javax.servlet.http.HttpServletRequest request) {
        BasicQuery query = new BasicQuery(request.getParameter("q"));
        return query.getQueryObject().toJson();
    }
}
