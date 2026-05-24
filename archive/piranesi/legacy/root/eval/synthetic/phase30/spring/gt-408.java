import java.util.Map;
import org.springframework.beans.MutablePropertyValues;
import org.springframework.beans.PropertyAccessorFactory;
import org.springframework.beans.BeanWrapper;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt408Controller {
    @PostMapping("/tickets")
    Gt408Ticket create(@RequestBody Map<String, Object> body) {
        Gt408Ticket ticket = new Gt408Ticket();
        BeanWrapper wrapper = PropertyAccessorFactory.forBeanPropertyAccess(ticket);
        wrapper.setPropertyValues(new MutablePropertyValues(body)); // sink
        return ticket;
    }
}

class Gt408Ticket {
    public String subject;
    public boolean approved;
}
