<?php
// Synthetic query facade only. The original proprietary PHP is mounted read-only.
namespace think\facade {
 final class Db {
  public static array $parents=[];
  public static function name($name){return new Query($name);}
 }
 final class Query {
  private string $table; private array $filters=[]; private array $sort=[];
  function __construct($table){$this->table=$table;}
  function where($key,$value){$this->filters[$key]=$value;return $this;}
  function field($unused){return $this;}
  function order($key,$direction){$this->sort[]=[$key,$direction];return $this;}
  function value($key){if($this->table==='group'&&$key==='info')return json_encode([1=>['channel'=>-4,'type'=>'channel','rate'=>'0']]);throw new \Exception('unexpected synthetic value lookup');}
  private function rows(){
   if($this->table!=='channel')throw new \Exception('unexpected synthetic table');
   $rows=array_values(array_filter(Db::$parents,function($row){foreach($this->filters as $key=>$value)if($row[$key]!=$value)return false;return true;}));
   usort($rows,function($a,$b){foreach($this->sort as [$key,$dir]){if($a[$key]!=$b[$key])return ($a[$key]<=>$b[$key])*($dir==='desc'?-1:1);}return 0;});
   return $rows;
  }
  function select(){return $this->rows();}
  function update($changes){foreach($this->rows() as $row)Db::$parents[$row['id']]=array_merge($row,$changes);return 1;}
 }
}
namespace {
 function isNullOrEmpty($v){return $v===null||$v==='';}
 require $argv[1];
 foreach([7,8,9] as $id)\think\facade\Db::$parents[$id]=['id'=>$id,'type'=>1,'plugin'=>'synthetic','status'=>1,'daystatus'=>0,'rate'=>'99.50','apptype'=>'','mode'=>0,'paymin'=>'','paymax'=>'','timestart'=>'','timestop'=>'','rate_limit_seconds'=>0,'rate_limit_count'=>0,'payfee_lessthan'=>null,'payfee_mincost'=>null,'usetime'=>0];
 $first=\app\lib\Channel::getSubmitInfo(1,'alipay',1,11,1);
 $other=\app\lib\Channel::getSubmitInfo(1,'alipay',2,22,1);
 \think\facade\Db::$parents[7]['status']=0;
 $removed=\app\lib\Channel::getSubmitInfo(1,'alipay',1,11,1);
 $actual=[$first['channel'],$other['channel'],$removed['channel']];
 if($actual!==[7,8,9])throw new \Exception('original Pro selection mismatch '.json_encode($actual));
 if($first['rate']!=='99.50')throw new \Exception('group zero did not inherit selected parent rate');
 echo json_encode(['synthetic_only'=>true,'pro_global_sequence'=>$actual,'group_zero_parent_rate'=>$first['rate'],'network'=>'disabled']),PHP_EOL;
}
