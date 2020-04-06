# s3usage

s3バケット毎の概算請求額を算出するツール

作ったきっかけなどは [こちら](https://qiita.com/miyaz/items/2bf28536558f4ec46e6b)にまとめています

## 使い方

```
./s3usage -p {profile} -v
```

* profileを指定しない場合は defaultプロファイルを使用します
* -v をつけるとストレージタイプ別の使用量も表示します

## その他

* 全リージョンの全バケットが対象です
* バケットサイズ／オブジェクト数はCloudWatchから取得しています
* コスト算出について
  * 実行時点の利用量を１ヶ月間継続した場合の概算請求額であり正確ではありません（ご利用は自己責任で）
  * ストレージ保存量に応じて課金される料金を対象としており、それ以外(リクエスト等)のコストは含みません
  * 概算請求額は東京リージョン料金(2020/04時点)で算出しています
* バージョニングについて
  * 以前のバージョンのオブジェクトやそのサイズもカウントされます
  * そのためツール結果とaws s3 ls --recursive等で得られるオブジェクト数は異なります
