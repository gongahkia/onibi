use tokio::{
    io::{AsyncReadExt, AsyncWriteExt, copy_bidirectional},
    net::{TcpListener, TcpStream},
};
use yeokcham_daemon::{TorSocksConnector, TorSocksTarget};
use yeokcham_protocol::EncryptedMessageEnvelope;

const ONION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion";
const MAILDROP_PORT: u16 = 4444;

#[tokio::test]
async fn synthetic_tor_maildrop_topology_forwards_an_encrypted_envelope() {
    let relay_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let proxy_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let relay_address = relay_listener.local_addr().unwrap();
    let connector = TorSocksConnector::new(proxy_listener.local_addr().unwrap()).unwrap();
    let target = TorSocksTarget::new(ONION.to_owned(), MAILDROP_PORT).unwrap();
    let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc3]).unwrap();
    let encoded = envelope.encode().unwrap();
    let expected = encoded.clone();

    let relay = tokio::spawn(async move {
        let (mut stream, _) = relay_listener.accept().await.unwrap();
        let mut length = [0; 2];
        stream.read_exact(&mut length).await.unwrap();
        let mut payload = vec![0; usize::from(u16::from_be_bytes(length))];
        stream.read_exact(&mut payload).await.unwrap();
        assert_eq!(payload, expected);
        stream.write_all(b"stored").await.unwrap();
    });
    let proxy = tokio::spawn(async move {
        let (mut client, _) = proxy_listener.accept().await.unwrap();
        let mut greeting = [0; 3];
        client.read_exact(&mut greeting).await.unwrap();
        assert_eq!(greeting, [5, 1, 0]);
        client.write_all(&[5, 0]).await.unwrap();
        let mut request = [0; 5];
        client.read_exact(&mut request).await.unwrap();
        assert_eq!(&request[..4], &[5, 1, 0, 3]);
        let mut hostname = vec![0; usize::from(request[4])];
        client.read_exact(&mut hostname).await.unwrap();
        let mut port = [0; 2];
        client.read_exact(&mut port).await.unwrap();
        assert_eq!(hostname, ONION.as_bytes());
        assert_eq!(u16::from_be_bytes(port), MAILDROP_PORT);
        client
            .write_all(&[5, 0, 0, 1, 127, 0, 0, 1, 0, 0])
            .await
            .unwrap();
        let mut relay = TcpStream::connect(relay_address).await.unwrap();
        copy_bidirectional(&mut client, &mut relay).await.unwrap();
    });

    let mut stream = connector.connect(&target).await.unwrap();
    stream
        .write_all(&u16::try_from(encoded.len()).unwrap().to_be_bytes())
        .await
        .unwrap();
    stream.write_all(&encoded).await.unwrap();
    let mut acknowledgement = [0; 6];
    stream.read_exact(&mut acknowledgement).await.unwrap();
    assert_eq!(&acknowledgement, b"stored");
    stream.shutdown().await.unwrap();

    relay.await.unwrap();
    proxy.await.unwrap();
}
