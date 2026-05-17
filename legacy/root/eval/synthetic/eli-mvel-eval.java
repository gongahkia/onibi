import org.mvel2.MVEL;

class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request) {
        return MVEL.eval(request.getParameter("expr"));
    }
}
