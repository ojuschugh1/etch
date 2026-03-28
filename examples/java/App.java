import java.net.*;
import java.net.http.*;

/**
 * Example: Using Etch with Java HttpClient.
 *
 * 1. Start etch:    ./etch record --port 8080
 * 2. Compile:       javac examples/java/App.java
 * 3. Run:           java -cp examples/java -Dhttp.proxyHost=localhost -Dhttp.proxyPort=8080 App
 * 4. Stop etch:     ctrl+c
 */
public class App {
    public static void main(String[] args) throws Exception {
        HttpClient client = HttpClient.newBuilder()
            .proxy(ProxySelector.of(new InetSocketAddress("localhost", 8080)))
            .build();

        // GET
        HttpRequest getReq = HttpRequest.newBuilder()
            .uri(URI.create("http://httpbin.org/get?page=1"))
            .build();
        HttpResponse<String> getResp = client.send(getReq, HttpResponse.BodyHandlers.ofString());
        System.out.println("GET /get -> " + getResp.statusCode());
        System.out.println(getResp.body().substring(0, Math.min(200, getResp.body().length())));

        // POST
        HttpRequest postReq = HttpRequest.newBuilder()
            .uri(URI.create("http://httpbin.org/post"))
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString("{\"name\":\"alice\"}"))
            .build();
        HttpResponse<String> postResp = client.send(postReq, HttpResponse.BodyHandlers.ofString());
        System.out.println("\nPOST /post -> " + postResp.statusCode());
        System.out.println(postResp.body().substring(0, Math.min(200, postResp.body().length())));
    }
}
